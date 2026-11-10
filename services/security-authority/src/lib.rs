//! The owner security authority's domain kernel.
//!
//! The runtime must hold exclusive custody of persistent authentication state.
//! No external interface may assert that a password or TOTP check succeeded
//! without invoking the cryptographic verifier in this crate.

pub mod canonical;
pub mod credentials;
pub mod sessions;
pub mod state;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Error {
    InvalidConfiguration,
    InvalidArguments,
    AuthenticationDenied,
    Unauthorized,
    ConfirmationRequired,
    Storage,
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "security authority denied request: {self:?}")
    }
}
impl std::error::Error for Error {}
