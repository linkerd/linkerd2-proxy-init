use anyhow::{bail, Result};
use clap::Parser;

/// Scan the pods in the current node to find those that have been injected by linkerd, and whose
/// linkerd-network-validator container has failed, and proceed to evict them so they can restart
/// and rety re-acquiring a proper network config.

#[derive(Parser)]
#[command(version)]
struct Args {
    #[arg(
        long,
        env = "LINKERD_REINITIALIZE_PODS_LOG_LEVEL",
        default_value = "linkerd_reinitialize_pods=info,warn"
    )]
    log_level: kubert::LogFilter,

    #[arg(
        long,
        env = "LINKERD_REINITIALIZE_PODS_LOG_FORMAT",
        default_value = "plain"
    )]
    log_format: kubert::LogFormat,

    #[arg(long, env = "LINKERD_REINITIALIZE_PODS_NODE_NAME")]
    node_name: String,

    #[arg(long, env = "LINKERD_REINITIALIZE_PODS_POD_NAME")]
    controller_pod_name: String,

    #[command(flatten)]
    client: kubert::ClientArgs,

    #[command(flatten)]
    admin: kubert::AdminArgs,
}

#[tokio::main]
async fn main() -> Result<()> {
    let Args {
        log_level,
        log_format,
        node_name,
        controller_pod_name,
        client,
        admin,
    } = Args::parse();

    let mut admin = admin.into_builder();
    admin.with_default_prometheus();

    let mut runtime = kubert::Runtime::builder()
        .with_log(log_level, log_format)
        .with_admin(admin)
        .with_client(client)
        .build()
        .await?;

    linkerd_reinitialize_pods::run(&mut runtime, node_name, controller_pod_name);

    // Block the main thread on the shutdown signal. Once it fires, wait for the background tasks to
    // complete before exiting.
    if runtime.run().await.is_err() {
        bail!("aborted");
    }

    Ok(())
}
