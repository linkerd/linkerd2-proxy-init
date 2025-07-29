use anyhow::Result;
use clap::Parser;
use kubert::Runtime;
use linkerd_cni_repair_controller::Metrics;

/// Scan the pods in the current node to find those that have been injected by linkerd, and whose
/// linkerd-network-validator container has failed, and proceed to evict them so they can restart
/// and rety re-acquiring a proper network config.

#[derive(Parser)]
#[command(version)]
struct Args {
    #[arg(
        long,
        env = "LINKERD_CNI_REPAIR_CONTROLLER_LOG_LEVEL",
        default_value = "linkerd_cni_repair_controller=info,warn"
    )]
    log_level: kubert::LogFilter,

    #[arg(
        long,
        env = "LINKERD_CNI_REPAIR_CONTROLLER_LOG_FORMAT",
        default_value = "plain"
    )]
    log_format: kubert::LogFormat,

    #[arg(long, env = "LINKERD_CNI_REPAIR_CONTROLLER_NODE_NAME")]
    node_name: String,

    #[arg(long, env = "LINKERD_CNI_REPAIR_CONTROLLER_POD_NAME")]
    controller_pod_name: String,

    #[command(flatten)]
    client: kubert::ClientArgs,

    #[command(flatten)]
    admin: kubert::AdminArgs,
}

#[tokio::main]
async fn main() -> Result<()> {
    if rustls::crypto::aws_lc_rs::default_provider()
        .install_default()
        .is_err()
    {
        anyhow::bail!("No other crypto provider should be installed yet");
    }

    let Args {
        log_level,
        log_format,
        node_name,
        controller_pod_name,
        client,
        admin,
    } = Args::parse();

    let mut prom = prometheus_client::registry::Registry::default();
    let metrics = Metrics::register(prom.sub_registry_with_prefix("linkerd_cni_repair_controller"));
    let mut rt = Runtime::builder()
        .with_log(log_level, log_format)
        .with_admin(admin.into_builder().with_prometheus(prom))
        .with_client(client)
        .build()
        .await?;

    let handle =
        linkerd_cni_repair_controller::run(&mut rt, node_name, controller_pod_name, metrics);

    // Block the main thread on the shutdown signal
    rt.run().await?;

    handle.abort();
    Ok(())
}
