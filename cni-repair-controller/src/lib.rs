use futures::{Stream, StreamExt};
use k8s_openapi::api::core::v1::{ObjectReference, Pod};
use kube::{
    api::DeleteParams,
    runtime::{
        events::{Event, EventType, Recorder, Reporter},
        watcher,
    },
    Client, Error, Resource, ResourceExt,
};
use kubert::Runtime;
use prometheus_client::{
    metrics::{counter::Counter, histogram::Histogram},
    registry::Registry,
};
use tokio::sync::mpsc::{self, error::TrySendError, Receiver, Sender};
use tokio::task::JoinHandle;
use tokio::time::{self, Duration, Instant};

// ERRNO 95: Operation not supported
const UNSUCCESSFUL_EXIT_CODE: i32 = 95;

// If the event channel capacity is reached, the event is dropped, but a new one will be emitted
// in the pod's next crashloop iteration
const EVENT_CHANNEL_CAPACITY: usize = 32;

const DATA_PLANE_LABEL: &str = "linkerd.io/control-plane-ns";
const EVENT_ACTION: &str = "Deleting";
const EVENT_REASON: &str = "LinkerdCNINotConfigured";

#[derive(Clone, Debug)]
pub struct Metrics {
    queue_overflow: Counter<u64>,
    pods_deleted: Counter<u64>,
    pods_delete_latency_seconds: Histogram,
    pods_delete_errors: Counter<u64>,
    pods_delete_timeouts: Counter<u64>,
    events_publish_latency_seconds: Histogram,
    events_publish_errors: Counter<u64>,
    events_publish_timeouts: Counter<u64>,
}

pub fn run(
    rt: &mut Runtime,
    node_name: String,
    controller_pod_name: String,
    metrics: Metrics,
) -> JoinHandle<()> {
    let pod_evts = rt.watch_all::<Pod>(
        watcher::Config::default()
            .labels(DATA_PLANE_LABEL)
            .fields(&format!("spec.nodeName={node_name}")),
    );
    let (tx, rx) = mpsc::channel(EVENT_CHANNEL_CAPACITY);
    tokio::spawn(process_events(pod_evts, tx, metrics.clone()));

    let client = rt.client();
    tokio::spawn(process_pods(client, controller_pod_name, rx, metrics))
}

async fn process_events(
    pod_evts: impl Stream<Item = watcher::Event<Pod>>,
    tx: Sender<ObjectReference>,
    metrics: Metrics,
) {
    tokio::pin!(pod_evts);
    while let Some(evt) = pod_evts.next().await {
        tracing::trace!(?evt);
        if let watcher::Event::Applied(pod) = evt {
            let status = if let Some(ref status) = pod.status {
                status.clone()
            } else {
                tracing::info!("Skipped, no status");
                continue;
            };

            let terminated = status
                .init_container_statuses
                .unwrap_or_default()
                .iter()
                .find(|status| status.name == "linkerd-network-validator")
                .and_then(|status| status.last_state.as_ref())
                .and_then(|status| status.terminated.as_ref())
                .map(|term| term.exit_code == UNSUCCESSFUL_EXIT_CODE)
                .unwrap_or(false);

            let deleting = pod.metadata.deletion_timestamp.is_some();

            if terminated && !deleting {
                let namespace = pod.namespace().unwrap();
                let name = pod.name_any();
                let object_ref = pod.object_ref(&());
                // this avoids blocking the event loop
                match tx.try_send(object_ref) {
                    Ok(_) => {}
                    Err(TrySendError::Full(_)) => {
                        // If a pod is in a failed state, it should continually be
                        // reported to be in CrashLoopBackoff; so it will naturally
                        // be retried.
                        tracing::debug!(%namespace, %name, "Dropped event (channel full)");
                        metrics.queue_overflow.inc();
                    }
                    Err(TrySendError::Closed(_)) => return,
                }
            }
        }
    }
}

async fn process_pods(
    client: Client,
    controller_pod_name: String,
    mut rx: Receiver<ObjectReference>,
    metrics: Metrics,
) {
    while let Some(object_ref) = rx.recv().await {
        let namespace = object_ref.namespace.clone().unwrap_or_default();
        let name = object_ref.name.clone().unwrap_or_default();
        let pods = kube::Api::<Pod>::namespaced(client.clone(), &namespace);
        let delete_params = DeleteParams {
            grace_period_seconds: Some(0),
            ..Default::default()
        };

        let t0 = Instant::now();
        let deleted = tokio::select! {
            res = pods.delete(&name, &delete_params) => res,
            _ = time::sleep(Duration::from_secs(1)) => {
                tracing::warn!(%namespace, %name, "Pod deletion timed out");
                metrics.pods_delete_timeouts.inc();
                continue;
            }
        };
        if let Err(err) = deleted {
            tracing::warn!(%err, %namespace, %name, "Error deleting pod");
            metrics.pods_delete_errors.inc();
            continue;
        }
        let latency = time::Instant::now() - t0;
        tracing::info!(%namespace, %name, "Deleted pod");
        metrics.pods_deleted.inc();
        metrics
            .pods_delete_latency_seconds
            .observe(latency.as_secs_f64());

        let t0 = Instant::now();
        let event = tokio::select! {
            res = publish_k8s_event(client.clone(), controller_pod_name.clone(), object_ref) => res,
            _ = time::sleep(Duration::from_secs(1)) => {
                tracing::warn!(%namespace, %name, "Event publishing timed out");
                metrics.events_publish_timeouts.inc();
                continue;
            }
        };
        if let Err(err) = event {
            tracing::warn!(%err, %namespace, %name, "Error publishing event");
            metrics.events_publish_errors.inc();
            continue;
        }
        let latency = time::Instant::now() - t0;
        metrics
            .events_publish_latency_seconds
            .observe(latency.as_secs_f64());
    }
}

async fn publish_k8s_event(
    client: Client,
    controller_pod_name: String,
    object_ref: ObjectReference,
) -> Result<(), Error> {
    let reporter = Reporter {
        controller: "linkerd-cni-repair-controller".into(),
        instance: Some(controller_pod_name),
    };
    let recorder = Recorder::new(client, reporter, object_ref);
    recorder
        .publish(Event {
            action: EVENT_ACTION.into(),
            reason: EVENT_REASON.into(),
            note: Some("Deleting pod to create a new one with proper CNI config".into()),
            type_: EventType::Normal,
            secondary: None,
        })
        .await
}

impl Metrics {
    pub fn register(prom: &mut Registry) -> Self {
        // Default values from go client
        // (https://github.com/prometheus/client_golang/blob/5d584e2717ef525673736d72cd1d12e304f243d7/prometheus/histogram.go#L68)
        let histogram_buckets = [
            0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
        ];

        let queue_overflow = Counter::<u64>::default();
        prom.register(
            "queue_overflow",
            "Incremented whenever the event processing queue overflows",
            queue_overflow.clone(),
        );

        let pods_deleted = Counter::<u64>::default();
        prom.register(
            "pods_deleted",
            "Number of pods deleted by the controller",
            pods_deleted.clone(),
        );

        let pods_delete_latency_seconds = Histogram::new(histogram_buckets.iter().cloned());
        prom.register(
            "pods_delete_latency_seconds",
            "Pod deletion latency distribution",
            pods_delete_latency_seconds.clone(),
        );

        let pods_delete_errors = Counter::<u64>::default();
        prom.register(
            "pods_delete_errors",
            "Incremented whenever the pod deletion call errors out",
            pods_delete_errors.clone(),
        );

        let pods_delete_timeouts = Counter::<u64>::default();
        prom.register(
            "pods_delete_timeout",
            "Incremented whenever the pod deletion call times out",
            pods_delete_timeouts.clone(),
        );

        let events_publish_latency_seconds = Histogram::new(histogram_buckets.iter().cloned());
        prom.register(
            "events_publish_latency_seconds",
            "Events publish latency distribution",
            events_publish_latency_seconds.clone(),
        );

        let events_publish_errors = Counter::<u64>::default();
        prom.register(
            "events_publish_errors",
            "Incremented whenever the event publishing call errors out",
            events_publish_errors.clone(),
        );

        let events_publish_timeouts = Counter::<u64>::default();
        prom.register(
            "events_publish_timeouts",
            "Incremented whenever the event publishing call times out",
            events_publish_timeouts.clone(),
        );

        Self {
            queue_overflow,
            pods_deleted,
            pods_delete_latency_seconds,
            pods_delete_errors,
            pods_delete_timeouts,
            events_publish_latency_seconds,
            events_publish_errors,
            events_publish_timeouts,
        }
    }
}

#[cfg(test)]
mod test {
    use super::*;
    use chrono::Utc;
    use k8s_openapi::api::core::v1::{
        ContainerState, ContainerStateTerminated, ContainerStatus, PodStatus,
    };
    use k8s_openapi::apimachinery::pkg::apis::meta::v1::{ObjectMeta, Time};
    use tokio::{
        sync::mpsc::error::TryRecvError,
        time::{self, Duration},
    };

    #[tokio::test]
    async fn test_process_events() {
        let mut prom = prometheus_client::registry::Registry::default();
        let metrics =
            Metrics::register(prom.sub_registry_with_prefix("linkerd_cni_repair_controller"));

        // This pod should be ignored
        let pod1 = Pod {
            metadata: ObjectMeta {
                name: Some("pod1".to_string()),
                namespace: Some("default".to_string()),
                ..Default::default()
            },
            ..Default::default()
        };

        // This pod should be processed
        let pod2 = Pod {
            metadata: ObjectMeta {
                name: Some("pod2".to_string()),
                namespace: Some("default".to_string()),
                ..Default::default()
            },
            status: Some(PodStatus {
                init_container_statuses: Some(vec![ContainerStatus {
                    name: "linkerd-network-validator".to_string(),
                    last_state: Some(ContainerState {
                        terminated: Some(ContainerStateTerminated {
                            exit_code: UNSUCCESSFUL_EXIT_CODE,
                            ..Default::default()
                        }),
                        ..Default::default()
                    }),
                    ..Default::default()
                }]),
                ..Default::default()
            }),
            ..Default::default()
        };

        // This pod should be ignored
        let pod3 = Pod {
            metadata: ObjectMeta {
                name: Some("pod2".to_string()),
                namespace: Some("default".to_string()),
                deletion_timestamp: Some(Time(Utc::now())),
                ..Default::default()
            },
            ..pod2.clone()
        };

        let (tx, mut rx) = mpsc::channel(EVENT_CHANNEL_CAPACITY);
        let stream = futures::stream::iter(vec![
            watcher::Event::Applied(pod1),
            watcher::Event::Applied(pod2),
            watcher::Event::Applied(pod3),
        ]);

        let process_events_handle = tokio::spawn(process_events(stream, tx, metrics));
        time::sleep(Duration::from_secs(2)).await;
        let msg = rx.try_recv();
        let object_ref = msg.unwrap();
        assert_eq!(object_ref.name, Some("pod2".to_string()));
        let msg = rx.try_recv();
        assert_eq!(msg, Err(TryRecvError::Disconnected));
        assert!(process_events_handle.is_finished());
    }
}
