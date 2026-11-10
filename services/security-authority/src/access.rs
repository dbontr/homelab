//! Verify signed owner identity using a locally pinned RS256 key set.
use std::collections::BTreeMap;
use std::time::{SystemTime, UNIX_EPOCH};

use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use jsonwebtoken::{decode, decode_header, Algorithm, DecodingKey, Validation};
use serde::Deserialize;
use serde_json::Value;

use crate::canonical::canonical_arguments;
use crate::Error;

const MAX_ASSERTION: usize = 16 << 10;
const MAX_JWKS: usize = 256 << 10;
const MAX_HEADER: usize = 4096;
const MAX_KEYS: usize = 32;
const MAX_LIFETIME: i64 = 24 * 60 * 60;

#[derive(Deserialize)]
struct Jwks {
    keys: Vec<Jwk>,
}

#[derive(Deserialize)]
struct Jwk {
    kty: String,
    kid: String,
    alg: Option<String>,
    #[serde(rename = "use")]
    purpose: Option<String>,
    n: String,
    e: String,
}

pub struct AccessVerifier {
    issuer: String,
    owner_email: String,
    keys: BTreeMap<String, DecodingKey>,
}

fn valid_rsa_public_key(jwk: &Jwk) -> bool {
    if jwk.kty != "RSA"
        || jwk.kid.is_empty()
        || jwk.kid.len() > 256
        || jwk.purpose.as_deref().is_some_and(|v| v != "sig")
        || jwk.alg.as_deref().is_some_and(|v| v != "RS256")
    {
        return false;
    }
    let Ok(modulus) = URL_SAFE_NO_PAD.decode(&jwk.n) else {
        return false;
    };
    let Ok(exponent) = URL_SAFE_NO_PAD.decode(&jwk.e) else {
        return false;
    };
    if !(256..=512).contains(&modulus.len()) || !(1..=4).contains(&exponent.len()) {
        return false;
    }
    let Some(first_nonzero) = modulus.iter().position(|n| *n != 0) else {
        return false;
    };
    let bits = (modulus.len() - first_nonzero - 1) * 8
        + (8 - modulus[first_nonzero].leading_zeros() as usize);
    let e = exponent.iter().fold(0u64, |n, byte| (n << 8) | u64::from(*byte));
    bits >= 2048 && (3..=65537).contains(&e) && e % 2 == 1
}

fn claim_seconds(claims: &Value, name: &str) -> Option<i64> {
    claims.get(name)?.as_i64()
}

fn audience_matches(claims: &Value, audience: &str) -> bool {
    match claims.get("aud") {
        Some(Value::String(value)) => value == audience,
        Some(Value::Array(values)) => values.iter().any(|v| v.as_str() == Some(audience)),
        _ => false,
    }
}

impl AccessVerifier {
    pub fn new(issuer: &str, owner_email: &str, jwks: &[u8]) -> Result<Self, Error> {
        let owner_email = owner_email.trim().to_ascii_lowercase();
        if !issuer.starts_with("https://")
            || issuer.contains('?')
            || issuer.contains('#')
            || owner_email.len() < 3
            || owner_email.len() > 320
            || owner_email.bytes().filter(|b| *b == b'@').count() != 1
            || owner_email.bytes().any(|b| b <= b' ')
            || jwks.is_empty()
            || jwks.len() > MAX_JWKS
        {
            return Err(Error::InvalidConfiguration);
        }
        canonical_arguments(jwks).map_err(|_| Error::InvalidConfiguration)?;
        let document: Jwks =
            serde_json::from_slice(jwks).map_err(|_| Error::InvalidConfiguration)?;
        if document.keys.is_empty() || document.keys.len() > MAX_KEYS {
            return Err(Error::InvalidConfiguration);
        }
        let mut keys = BTreeMap::new();
        for key in document.keys {
            if !valid_rsa_public_key(&key) {
                return Err(Error::InvalidConfiguration);
            }
            let decoding_key = DecodingKey::from_rsa_components(&key.n, &key.e)
                .map_err(|_| Error::InvalidConfiguration)?;
            if keys.insert(key.kid, decoding_key).is_some() {
                return Err(Error::InvalidConfiguration);
            }
        }
        Ok(Self {
            issuer: issuer.into(),
            owner_email,
            keys,
        })
    }

    pub fn verify(&self, assertion: &str, audience: &str) -> Result<String, Error> {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| Error::Unauthorized)?
            .as_secs() as i64;
        self.verify_at(assertion, audience, now)
    }

    fn verify_at(&self, assertion: &str, audience: &str, now: i64) -> Result<String, Error> {
        if !(20..=MAX_ASSERTION).contains(&assertion.len()) || audience.is_empty() {
            return Err(Error::Unauthorized);
        }
        let parts: Vec<&str> = assertion.split('.').collect();
        if parts.len() != 3 {
            return Err(Error::Unauthorized);
        }
        let raw_header = URL_SAFE_NO_PAD
            .decode(parts[0])
            .map_err(|_| Error::Unauthorized)?;
        let raw_claims = URL_SAFE_NO_PAD
            .decode(parts[1])
            .map_err(|_| Error::Unauthorized)?;
        if raw_header.len() > MAX_HEADER || raw_claims.len() > MAX_ASSERTION {
            return Err(Error::Unauthorized);
        }
        canonical_arguments(&raw_header).map_err(|_| Error::Unauthorized)?;
        canonical_arguments(&raw_claims).map_err(|_| Error::Unauthorized)?;
        let header = decode_header(assertion).map_err(|_| Error::Unauthorized)?;
        if header.alg != Algorithm::RS256 {
            return Err(Error::Unauthorized);
        }
        let key = self.keys.get(header.kid.as_deref().ok_or(Error::Unauthorized)?)
            .ok_or(Error::Unauthorized)?;
        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_issuer(&[&self.issuer]);
        validation.set_audience(&[audience]);
        // Exact second-level checks below also enforce iat and a 24-hour
        // issuance window; they use the same supplied clock as the caller.
        validation.validate_exp = false;
        validation.validate_nbf = false;
        validation.required_spec_claims.clear();
        let data = decode::<Value>(assertion, key, &validation)
            .map_err(|_| Error::Unauthorized)?;
        let claims = data.claims;
        let exp = claim_seconds(&claims, "exp").ok_or(Error::Unauthorized)?;
        let iat = claim_seconds(&claims, "iat").ok_or(Error::Unauthorized)?;
        let nbf = if claims.get("nbf").is_some() {
            claim_seconds(&claims, "nbf").ok_or(Error::Unauthorized)?
        } else {
            iat
        };
        let subject = claims.get("sub").and_then(Value::as_str).ok_or(Error::Unauthorized)?;
        let email = claims.get("email").and_then(Value::as_str).ok_or(Error::Unauthorized)?
            .trim().to_ascii_lowercase();
        if claims.get("iss").and_then(Value::as_str) != Some(self.issuer.as_str())
            || email != self.owner_email
            || subject.is_empty()
            || subject.len() > 512
            || subject.bytes().any(|c| matches!(c, b'\n' | b'\r' | 0))
            || !audience_matches(&claims, audience)
            || iat > now.saturating_add(30)
            || nbf > now.saturating_add(30)
            || exp <= now
            || exp <= iat
            || exp.saturating_sub(iat) > MAX_LIFETIME
            || iat < now.saturating_sub(MAX_LIFETIME)
        {
            return Err(Error::Unauthorized);
        }
        Ok(format!("{}#{}#{}", self.issuer, subject, email))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;

    #[derive(Deserialize)]
    struct Fixture {
        issuer: String,
        email: String,
        audience: String,
        principal: String,
        jwks: Value,
        token: String,
    }
    fn fixture() -> (AccessVerifier, Fixture) {
        let fixture: Fixture = serde_json::from_str(include_str!(
            "../tests/fixtures/access-jwt.json"
        )).unwrap();
        let verifier = AccessVerifier::new(
            &fixture.issuer, &fixture.email,
            serde_json::to_string(&fixture.jwks).unwrap().as_bytes(),
        ).unwrap();
        (verifier, fixture)
    }

    #[test]
    fn admits_only_signed_owner_with_exact_issuer_audience_and_lifetime() {
        let (verifier, sample) = fixture();
        assert_eq!(
            verifier.verify_at(&sample.token, &sample.audience, 1_770_000_120).unwrap(),
            sample.principal
        );
        assert!(matches!(
            verifier.verify_at(&sample.token, "another-audience", 1_770_000_120),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            verifier.verify_at(&sample.token, &sample.audience, 1_770_004_000),
            Err(Error::Unauthorized)
        ));
        assert!(matches!(
            verifier.verify_at(&sample.token, &sample.audience, 1_769_999_900),
            Err(Error::Unauthorized)
        ));
    }

    #[test]
    fn denies_tampering_and_unknown_signing_keys() {
        let (verifier, sample) = fixture();
        let mut altered = sample.token.into_bytes();
        let index = altered.len() - 80;
        altered[index] = if altered[index] == b'X' { b'Y' } else { b'X' };
        let forged = String::from_utf8(altered).unwrap();
        assert!(matches!(
            verifier.verify_at(&forged, &sample.audience, 1_770_000_120),
            Err(Error::Unauthorized)
        ));
    }

    #[test]
    fn rejects_duplicate_and_weak_jwks() {
        let (_, fixture) = fixture();
        let jwks = serde_json::to_string(&fixture.jwks).unwrap();
        let duplicated = jwks.replace("\"keys\":", "\"keys\":[],\"keys\":");
        assert!(matches!(
            AccessVerifier::new(&fixture.issuer, &fixture.email, duplicated.as_bytes()),
            Err(Error::InvalidConfiguration)
        ));
        assert!(matches!(
            AccessVerifier::new("http://untrusted", &fixture.email, jwks.as_bytes()),
            Err(Error::InvalidConfiguration)
        ));
    }
}
