//! Restricted host-side privileged operations, separated from the public MCP.
pub mod audit;
pub mod authorization;
pub mod canonical;
pub mod executor;
pub mod policy;
pub mod release;
pub mod transport;
pub mod workspace;

#[derive(Debug)]
pub enum Error {
    Configuration(&'static str),
    Denied(&'static str),
    Unavailable(&'static str),
    Storage(&'static str),
    Protocol(&'static str),
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let message = match self {
            Self::Configuration(m) | Self::Denied(m) | Self::Unavailable(m)
            | Self::Storage(m) | Self::Protocol(m) => m,
        };
        f.write_str(message)
    }
}

impl std::error::Error for Error {}

pub fn random_hex<const N: usize>() -> Result<String, Error> {
    let mut data = [0u8; N];
    getrandom::fill(&mut data).map_err(|_| Error::Unavailable("random source unavailable"))?;
    Ok(hex::encode(data))
}

pub fn valid_host(s: &str) -> bool {
    !s.is_empty() && s.len() <= 63 && s.as_bytes()[0].is_ascii_lowercase()
        && s.bytes().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == b'-')
}

pub fn valid_hex(s: &str, length: usize) -> bool {
    s.len() == length && s.bytes().all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}
