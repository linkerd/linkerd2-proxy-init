use futures_util::{Stream, StreamExt};
use k8s_openapi::api::core::v1::Pod;
use kube::{
    runtime::{
        events::{Event, EventType, Recorder, Reporter},
        watcher,
    },
    Client, Error, Resource, ResourceExt,
};
use kubert::Runtime;
use prometheus_client::{metrics::counter::Counter, registry::Registry};
use tokio::sync::mpsc::{self, error::TrySendError, Receiver, Sender};

// ERRNO 95: Operation not supported
pub const UNSUCCESSFUL_EXIT_CODE: i32 = 95;

// If the event channel capacity is reached, the event is dropped, but a new one will be emitted
// in the pod's next crashloop iteration
const EVENT_CHANNEL_CAPACITY: usize = 32;

const DATA_PLANE_LABEL: &str = "linkerd.io/control-plane-ns";
const CONDITION_EVICTED_REASON: &str = "EvictionByEvictionAPI";
const EVENT_ACTION: &str = "Evicting";
const EVENT_REASON: &str = "LinkerdCNINotConfigured";

#[derive(Clone, Debug)]
pub struct Metrics {
    queue_overflow: Counter<u64>,
    evicted_pods: Counter<u64>,
}

pub fn run(rt: &mut Runtime, node_name: String, controller_pod_name: String, metrics: Metrics) {
    let pod_evts = rt.watch_all::<Pod>(
        watcher::Config::default()
            .labels(DATA_PLANE_LABEL)
            .fields(&format!("spec.nodeName={node_name}")),
    );
    let (tx, rx) = mpsc::channel(EVENT_CHANNEL_CAPACITY);
    tokio::spawn(process_events(pod_evts, tx, metrics.clone()));

    let client = rt.client();
    tokio::spawn(process_pods(client, controller_pod_name, rx, metrics));
}

async fn process_events(
    pod_evts: impl Stream<Item = watcher::Event<Pod>>,
    tx: Sender<Pod>,
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

            let evicted = status
                .conditions
                .as_ref()
                .and_then(|conds| {
                    conds.iter().find(|cond| {
                        cond.reason
                            .as_ref()
                            .is_some_and(|reason| reason == CONDITION_EVICTED_REASON)
                    })
                })
                .is_some();

            if terminated && !evicted {
                let namespace = pod.namespace().unwrap();
                let name = pod.name_any();
                // this avoids blocking the event loop
                match tx.try_send(pod.clone()) {
                    Ok(_) => {}
                    Err(TrySendError::Full(_)) => {
                        tracing::warn!(
                            name = format!("{namespace}/{name}"),
                            "Dropped event (channel full)"
                        );
                        metrics.queue_overflow.inc();
                    }
                    Err(TrySendError::Closed(_)) => tracing::warn!(
                        name = format!("{namespace}/{name}"),
                        "Dropped event (channel closed or dropped)"
                    ),
                }
            }
        }
    }
}

async fn process_pods(
    client: Client,
    controller_pod_name: String,
    mut rx: Receiver<Pod>,
    metrics: Metrics,
) {
    while let Some(pod) = rx.recv().await {
        let namespace = pod.namespace().unwrap();
        let name = pod.name_any();
        let pods = kube::Api::<Pod>::namespaced(client.clone(), &namespace);
        let evict_res = pods.evict(&name, &Default::default()).await;
        match evict_res {
            Ok(_) => {
                tracing::info!(name = format!("{namespace}/{name}"), "Evicting pod");
                metrics.evicted_pods.inc();
                if let Err(err) =
                    publish_k8s_event(client.clone(), controller_pod_name.clone(), &pod).await
                {
                    tracing::warn!(%err, name = format!("{namespace}/{name}"), "Error publishing event");
                }
            }
            Err(err) => {
                tracing::warn!(%err, name = format!("{namespace}/{name}"), "Error evicting pod")
            }
        }
    }
}

async fn publish_k8s_event(
    client: Client,
    controller_pod_name: String,
    pod: &Pod,
) -> Result<(), Error> {
    let reporter = Reporter {
        controller: "linkerd-reinitialize-pods".into(),
        instance: Some(controller_pod_name),
    };
    let reference = pod.object_ref(&());
    let recorder = Recorder::new(client, reporter, reference);
    recorder
        .publish(Event {
            action: EVENT_ACTION.into(),
            reason: EVENT_REASON.into(),
            note: Some("Evicting pod to create a new one with proper CNI config".into()),
            type_: EventType::Normal,
            secondary: None,
        })
        .await
}

impl Metrics {
    pub fn register(prom: &mut Registry) -> Self {
        let queue_overflow = Counter::<u64>::default();
        prom.register(
            "queue_overflow",
            "Incremented whenever the event processing queue overflows",
            queue_overflow.clone(),
        );
        let evicted_pods = Counter::<u64>::default();
        prom.register(
            "evicted",
            "Number of pods evicted by the controller",
            evicted_pods.clone(),
        );

        Self {
            queue_overflow,
            evicted_pods,
        }
    }
}
