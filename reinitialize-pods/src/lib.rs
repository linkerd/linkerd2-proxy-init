use futures_util::StreamExt;
use k8s_openapi::api::core::v1::Pod;
use kube::{
    runtime::{
        events::{Event, EventType, Recorder, Reporter},
        watcher,
    },
    Client, Error, Resource,
};
use kubert::Runtime;

// ERRNO 95: Operation not supported
pub const UNSUCCESSFUL_EXIT_CODE: i32 = 95;

const DATA_PLANE_LABEL: &str = "linkerd.io/control-plane-ns";
const CONDITION_EVICTED_REASON: &str = "EvictionByEvictionAPI";
const EVENT_ACTION: &str = "Evicting";
const EVENT_REASON: &str = "LinkerdCNINotConfigured";

pub fn run(runtime: &mut Runtime, node_name: String, controller_pod_name: String) {
    let client = runtime.client().clone();
    let pod_evts = runtime.watch_all::<Pod>(
        watcher::Config::default()
            .labels(DATA_PLANE_LABEL)
            .fields(&format!("spec.nodeName={node_name}")),
    );

    tokio::spawn(async move {
        tokio::pin!(pod_evts);
        while let Some(evt) = pod_evts.next().await {
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
                    let namespace = pod.metadata.namespace.as_ref().unwrap();
                    let name = pod.metadata.name.as_ref().unwrap();
                    let pods = kube::Api::<Pod>::namespaced(client.clone(), namespace);
                    let evict_res = pods.evict(name, &Default::default()).await;
                    match evict_res {
                        Ok(_) => {
                            tracing::info!(name = format!("{namespace}/{name}"), "Evicting pod");
                            if let Err(err) =
                                publish_event(client.clone(), controller_pod_name.clone(), &pod)
                                    .await
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
        }
    });
}

async fn publish_event(
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
