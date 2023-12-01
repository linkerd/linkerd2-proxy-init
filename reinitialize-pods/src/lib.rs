use futures_util::StreamExt;
use k8s_openapi::api::core::v1::Pod;
use kube::runtime::watcher;
use kubert::Runtime;

// ERRNO 95: Operation not supported
pub const UNSUCCESSFUL_EXIT_CODE: i32 = 95;

const DATA_PLANE_LABEL: &str = "linkerd.io/control-plane-ns";
const CONDITION_EVICTED_REASON: &str = "EvictionByEvictionAPI";

pub fn run(runtime: &mut Runtime, node_name: String) {
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
                let maybe_terminated = pod
                    .status
                    .clone()
                    .and_then(|x| x.init_container_statuses)
                    .and_then(|x| x.into_iter().next())
                    .filter(|x| x.name == "linkerd-network-validator")
                    .and_then(|x| x.last_state)
                    .and_then(|x| x.terminated)
                    .filter(|x| x.exit_code == UNSUCCESSFUL_EXIT_CODE);

                let maybe_already_evicting =
                    pod.status.clone().and_then(|x| x.conditions).and_then(|x| {
                        x.into_iter().find(|y| {
                            y.reason
                                .as_ref()
                                .is_some_and(|z| z == CONDITION_EVICTED_REASON)
                        })
                    });

                if maybe_terminated.is_some() && maybe_already_evicting.is_none() {
                    let pods = kube::Api::<Pod>::namespaced(
                        client.clone(),
                        &pod.metadata.namespace.unwrap(),
                    );
                    let evict_res = pods
                        .evict(&pod.metadata.name.clone().unwrap(), &Default::default())
                        .await;
                    match evict_res {
                        Ok(_) => tracing::info!("Evicting pod {}", pod.metadata.name.unwrap()),
                        Err(err) => tracing::warn!("Error evicting pod: {:?}", err),
                    }
                }
            }
        }
    });
}
