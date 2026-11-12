//! Credential verification compatible with the existing owner enrollment format.
use base64::{engine::general_purpose::STANDARD_NO_PAD, Engine};
use data_encoding::BASE32_NOPAD;
use hmac::{Hmac, Mac};
use pbkdf2::pbkdf2_hmac;
use serde::{Deserialize, Serialize};
use sha1::Sha1;
use sha2::Sha256;
use subtle::ConstantTimeEq;
use zeroize::Zeroize;

use crate::Error;

const MIN_ITERATIONS: u32 = 310_000;
const MAX_ITERATIONS: u32 = 2_000_000;

#[derive(Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct PassphraseRecord {
    pub version: u32,
    pub iterations: u32,
    pub salt: String,
    pub digest: String,
}

pub struct Factors {
    salt: [u8; 24],
    digest: [u8; 32],
    iterations: u32,
    totp_key: Vec<u8>,
}

impl Drop for Factors {
    fn drop(&mut self) {
        self.salt.zeroize();
        self.digest.zeroize();
        self.totp_key.zeroize();
    }
}

impl Factors {
    pub fn new(record: PassphraseRecord, base32_secret: &str) -> Result<Self, Error> {
        if record.version != 1 || !(MIN_ITERATIONS..=MAX_ITERATIONS).contains(&record.iterations) {
            return Err(Error::InvalidConfiguration);
        }
        let salt = STANDARD_NO_PAD
            .decode(record.salt.as_bytes())
            .map_err(|_| Error::InvalidConfiguration)?;
        let digest = STANDARD_NO_PAD
            .decode(record.digest.as_bytes())
            .map_err(|_| Error::InvalidConfiguration)?;
        if salt.len() != 24 || digest.len() != 32 {
            return Err(Error::InvalidConfiguration);
        }
        let secret = base32_secret.trim().to_ascii_uppercase();
        if !(32..=208).contains(&secret.len()) {
            return Err(Error::InvalidConfiguration);
        }
        let key = BASE32_NOPAD
            .decode(secret.as_bytes())
            .map_err(|_| Error::InvalidConfiguration)?;
        if !(20..=128).contains(&key.len()) {
            return Err(Error::InvalidConfiguration);
        }
        let mut decoded_salt = [0; 24];
        let mut decoded_digest = [0; 32];
        decoded_salt.copy_from_slice(&salt);
        decoded_digest.copy_from_slice(&digest);
        Ok(Self {
            salt: decoded_salt,
            digest: decoded_digest,
            iterations: record.iterations,
            totp_key: key,
        })
    }

    fn password_matches(&self, password: &str) -> bool {
        if password.len() > 1024 {
            return false;
        }
        let mut derived = [0u8; 32];
        pbkdf2_hmac::<Sha256>(
            password.as_bytes(),
            &self.salt,
            self.iterations,
            &mut derived,
        );
        let equal: bool = derived.ct_eq(&self.digest).into();
        derived.zeroize();
        equal
    }

    fn totp_for(&self, step: i64) -> String {
        let mut mac = Hmac::<Sha1>::new_from_slice(&self.totp_key)
            .expect("HMAC accepts arbitrary secret lengths");
        mac.update(&(step as u64).to_be_bytes());
        let value = mac.finalize().into_bytes();
        let offset = usize::from(value[19] & 0x0f);
        let truncated = u32::from_be_bytes(
            value[offset..offset + 4]
                .try_into()
                .expect("four-byte truncation"),
        ) & 0x7fff_ffff;
        format!("{:06}", truncated % 1_000_000)
    }

    #[cfg(test)]
    pub(crate) fn totp_test_code(&self, step: i64) -> String {
        self.totp_for(step)
    }

    fn matching_step(&self, code: &str, now_seconds: i64, last_step: i64) -> Option<i64> {
        if code.len() != 6 || !code.bytes().all(|b| b.is_ascii_digit()) {
            return None;
        }
        let counter = now_seconds.div_euclid(30);
        for step in [counter - 1, counter, counter + 1] {
            if step >= 0 && step > last_step {
                let generated = self.totp_for(step);
                if bool::from(generated.as_bytes().ct_eq(code.as_bytes())) {
                    return Some(step);
                }
            }
        }
        None
    }

    /// Persist every password/OTP attempt before returning an approval.
    /// A failed persistence operation cannot advance authentication state.
    pub fn verify(
        &self,
        state: &mut AuthenticatorState,
        password: &str,
        code: &str,
        now_seconds: i64,
        persist: impl FnOnce(&AuthenticatorState) -> Result<(), Error>,
    ) -> Result<(), Error> {
        if now_seconds < state.locked_until {
            return Err(Error::AuthenticationDenied);
        }
        let password_ok = self.password_matches(password);
        let step = self.matching_step(code, now_seconds, state.last_totp_step);
        let accepted = password_ok && step.is_some();
        let mut next = state.clone();
        if accepted {
            next.last_totp_step = step.expect("accepted authentication has an OTP step");
            next.failures = 0;
            next.locked_until = 0;
        } else {
            next.failures += 1;
            if next.failures >= 5 {
                next.failures = 0;
                next.locked_until = now_seconds + 300;
            }
        }
        persist(&next)?;
        *state = next;
        if accepted {
            Ok(())
        } else {
            Err(Error::AuthenticationDenied)
        }
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AuthenticatorState {
    pub last_totp_step: i64,
    pub failures: u8,
    pub locked_until: i64,
}

impl Default for AuthenticatorState {
    fn default() -> Self {
        Self {
            last_totp_step: -1,
            failures: 0,
            locked_until: 0,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const PASSPHRASE: &str = "correct horse battery staple";
    fn fixture() -> Factors {
        Factors::new(
            PassphraseRecord {
                version: 1,
                iterations: 310_000,
                salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
                digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
            },
            "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
        )
        .unwrap()
    }
    #[test]
    fn matches_existing_pbkdf2_password_format() {
        let factors = fixture();
        assert!(factors.password_matches(PASSPHRASE));
        assert!(!factors.password_matches("incorrect passphrase"));
    }
    #[test]
    fn matches_rfc_6238_six_digit_totp() {
        let factors = fixture();
        assert_eq!(factors.totp_for(59 / 30), "287082");
    }
    #[test]
    fn rejects_replay_and_fails_closed_on_storage_errors() {
        let factors = fixture();
        let mut state = AuthenticatorState::default();
        let code = factors.totp_for(100);
        assert!(factors
            .verify(&mut state, PASSPHRASE, &code, 3_000, |_| Ok(()))
            .is_ok());
        assert_eq!(state.last_totp_step, 100);
        assert!(matches!(
            factors.verify(&mut state, PASSPHRASE, &code, 3_000, |_| Ok(())),
            Err(Error::AuthenticationDenied)
        ));
        let old = state.clone();
        let next_code = factors.totp_for(101);
        assert!(matches!(
            factors.verify(&mut state, PASSPHRASE, &next_code, 3_030, |_| Err(
                Error::Storage
            )),
            Err(Error::Storage)
        ));
        assert_eq!(state, old);
    }
    #[test]
    fn five_failed_attempts_lock_authentication_for_five_minutes() {
        let factors = fixture();
        let mut state = AuthenticatorState::default();
        let code = factors.totp_for(100);
        for index in 1..=5 {
            assert_eq!(
                factors.verify(&mut state, "not-the-passphrase", &code, 3_000, |_| Ok(())),
                Err(Error::AuthenticationDenied)
            );
            if index < 5 {
                assert_eq!(state.failures, index);
            }
        }
        assert_eq!(state.failures, 0);
        assert_eq!(state.locked_until, 3_300);
        assert_eq!(
            factors.verify(&mut state, PASSPHRASE, &code, 3_299, |_| Ok(())),
            Err(Error::AuthenticationDenied)
        );
        let later = factors.totp_for(110);
        assert_eq!(
            factors.verify(&mut state, PASSPHRASE, &later, 3_300, |_| Ok(())),
            Ok(())
        );
        assert_eq!(state.locked_until, 0);
        assert_eq!(state.last_totp_step, 110);
    }

    #[test]
    fn rejects_invalid_configurations() {
        let record = PassphraseRecord {
            version: 1,
            iterations: 1,
            salt: "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4".into(),
            digest: "l9m+PJ5k/gZlE6OL9Ek9YfJ7L5ajP+9F+d5Z1mp4TXg".into(),
        };
        assert!(matches!(
            Factors::new(record, "invalid"),
            Err(Error::InvalidConfiguration)
        ));
    }
}
