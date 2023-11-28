use anyhow::{anyhow, bail, ensure, Context, Result};
use bytes::{Bytes, BytesMut};
use clap::Parser;
use linkerd_reinitialize_pods::UNSUCCESSFUL_EXIT_CODE;
use rand::distributions::{Alphanumeric, DistString};
use std::{net::SocketAddr, process::exit, time};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt},
    net::{TcpListener, TcpStream},
    signal::unix as signal,
};
use tracing::{debug, error, info, Instrument};

/// Validate that a container's networking is setup for the Linkerd proxy
///
/// Validation is done by binding a server on the proxy's outbound port and
/// initiating a connection to an arbitrary (hopefully unroutable) address. If
/// networking has been configured properly, the connection should be
/// established to the server.
#[derive(Parser)]
#[clap(version)]
struct Args {
    #[clap(
        long,
        env = "LINKERD_NETWORK_VALIDATOR_LOG_LEVEL",
        default_value = "info"
    )]
    log_level: kubert::LogFilter,

    #[clap(
        long,
        env = "LINKERD_NETWORK_VALIDATOR_LOG_FORMAT",
        default_value = "plain"
    )]
    log_format: kubert::LogFormat,

    #[clap(value_parser = parse_timeout, long, default_value = "10s")]
    timeout: std::time::Duration,

    /// Address to which connections are supposed to be redirected by the
    /// operating system
    #[clap(long, default_value = "0.0.0.0:4140")]
    listen_addr: SocketAddr,

    /// Address to which the client will attempt to connect
    #[clap(long, default_value = "192.0.2.2:1404")]
    connect_addr: SocketAddr,
}

#[tokio::main(flavor = "current_thread")]
async fn main() {
    let Args {
        log_level,
        log_format,
        timeout,
        listen_addr,
        connect_addr,
    } = Args::parse();

    log_format
        .try_init(log_level)
        .expect("must configure logging");

    let mut sigint = signal::signal(signal::SignalKind::interrupt()).expect("must register SIGINT");
    let mut sigterm =
        signal::signal(signal::SignalKind::terminate()).expect("must register SIGTERM");

    tokio::select! {
        biased;

        // If validation fails, exit with an error.
        res = validate(listen_addr, connect_addr) => {
            if let Err(error) = res {
                error!(%error);
                exit(UNSUCCESSFUL_EXIT_CODE);
            }
            info!("Validated");
        }

        // If validation doesn't complete in a timely manner, exit with an
        // error.
        () = tokio::time::sleep(timeout) => {
            error!(?timeout, "Failed to validate networking configuration. \
            Please ensure iptables rules are rewriting traffic as expected.");
            exit(UNSUCCESSFUL_EXIT_CODE);
        }

        // If the process is terminated by a signal, exit with an error.
        _ = sigint.recv() => {
            error!("Killed by SIGINT");
            exit(UNSUCCESSFUL_EXIT_CODE);
        }
        _ = sigterm.recv() => {
            error!("Killed by SIGTERM");
            exit(UNSUCCESSFUL_EXIT_CODE);
        }
    }
}

// === Validation ===

/// Validates that connecting to `connect_addr` actually connects to
/// `listen_addr`.
///
/// This validates that the operating system (i.e. iptables) is configured to
/// redirect connections to a Linkerd proxy (with an outbound port of
/// `listen_addr`).
async fn validate(listen_addr: SocketAddr, connect_addr: SocketAddr) -> Result<()> {
    // First, bind the server address so that all connections can be processed
    // by the server.
    let listener = TcpListener::bind(listen_addr).await?;
    info!("Listening for connections on {listen_addr}");

    // Generate a random token to be sent from the server. Clients use the
    // server response to ensure that it is connecting to this process.
    let token = format!(
        "{}\n",
        Alphanumeric.sample_string(&mut rand::thread_rng(), 63)
    );
    debug!(?token);
    let token = Bytes::from(token);

    // Spawn a server on a background task that writes the response and then
    // closes client the connection.
    tokio::spawn(serve(listener, token.clone()).in_current_span());

    // Connect to an arbitrary address, read data from the connection, and fail
    // if it doesn't match the server's token.
    info!("Connecting to {connect_addr}");
    let data = connect(connect_addr, token.len()).await.map_err(|error| {
        error!(%error, "Unable to connect to validator. Please ensure iptables \
                        rules are rewriting traffic as expected");
        error
    })?;
    debug!(data = ?String::from_utf8_lossy(&data), size = data.len());
    ensure!(
        data == token,
        "expected client to receive {:?}; got {:?} instead",
        String::from_utf8_lossy(&token),
        String::from_utf8_lossy(&data),
    );
    Ok(())
}

/// Accepts connections from `listener`, writes `token` to the socket, and then closes the
/// connection.
#[tracing::instrument(level = "debug", skip_all)]
async fn serve(listener: TcpListener, token: Bytes) {
    loop {
        let (mut socket, client_addr) = listener
            .accept()
            .await
            .expect("Failed to establish connection");
        let token = token.clone();
        tokio::spawn(
            async move {
                debug!("Accepted");
                // We expect this write to complete instantaneously, so a timeout is not needed
                // here.
                match socket.write_all(&token).await {
                    Ok(()) => debug!(bytes = token.len(), "Wrote message to client"),
                    Err(error) => error!(%error, "Failed to write bytes to client"),
                }
            }
            .instrument(tracing::info_span!("conn", client.addr = %client_addr)),
        );
    }
}

/// Connects to the target address and reads exactly `size` bytes.
#[tracing::instrument(level = "debug", skip_all)]
async fn connect(addr: SocketAddr, size: usize) -> Result<Bytes> {
    let mut socket = TcpStream::connect(addr).await?;
    debug!(client.addr = %socket.local_addr()?, "Connected");

    socket
        .readable()
        .await
        .context("cannot read from client socket")?;

    let mut buf = BytesMut::with_capacity(size);
    while buf.len() != size {
        let size = socket.read_buf(&mut buf).await?;
        debug!(bytes = %size, "Read message from server");
        if size == 0 {
            break;
        }
    }
    Ok(buf.freeze())
}

// === Utility functions ===

pub fn parse_timeout(s: &str) -> Result<time::Duration> {
    let s = s.trim();
    let offset = s
        .rfind(|c: char| c.is_ascii_digit())
        .ok_or_else(|| anyhow!("{} does not contain a timeout duration value", s))?;
    let (magnitude, unit) = s.split_at(offset + 1);
    let magnitude = magnitude.parse::<u64>()?;

    let mul = match unit {
        "" if magnitude == 0 => 0,
        "ms" => 1,
        "s" => 1000,
        "m" => 1000 * 60,
        "h" => 1000 * 60 * 60,
        "d" => 1000 * 60 * 60 * 24,
        _ => bail!(
            "invalid duration unit {} (expected one of 'ms', 's', 'm', 'h', or 'd')",
            unit
        ),
    };

    let ms = magnitude
        .checked_mul(mul)
        .ok_or_else(|| anyhow!("Timeout value {} overflows when converted to 'ms'", s))?;
    Ok(time::Duration::from_millis(ms))
}

#[cfg(test)]
mod tests {
    use crate::parse_timeout;
    use std::time;

    #[test]
    fn test_parse_timeout_invalid() {
        assert!(parse_timeout("120").is_err());
        assert!(parse_timeout("s").is_err());
        assert!(parse_timeout("foobars").is_err());
        assert!(parse_timeout("18446744073709551615s").is_err())
    }

    #[test]
    fn test_parse_timeout_seconds() {
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0ms").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0s").unwrap());
        assert_eq!(time::Duration::from_secs(0), parse_timeout("0m").unwrap());

        assert_eq!(
            time::Duration::from_secs(120),
            parse_timeout("120s").unwrap()
        );
        assert_eq!(
            time::Duration::from_secs(120),
            parse_timeout("120000ms").unwrap(),
        );
        assert_eq!(time::Duration::from_secs(120), parse_timeout("2m").unwrap());
        assert_eq!(
            time::Duration::from_secs(7200),
            parse_timeout("2h").unwrap()
        );
        assert_eq!(
            time::Duration::from_secs(172800),
            parse_timeout("2d").unwrap()
        );
    }
}
