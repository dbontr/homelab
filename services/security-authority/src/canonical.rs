//! Canonical arguments with recursive duplicate-key rejection.
use std::collections::BTreeSet;
use std::fmt;

use serde::de::{self, MapAccess, SeqAccess, Visitor};
use serde::Deserialize;
use serde_json::Value;
use sha2::{Digest, Sha256};

use crate::Error;

// The first pass validates every object key independently of Value parsing.
// With arbitrary_precision, serde_json represents some numeric tokens using
// an internal map visitor; decoding into Value in a second pass preserves the
// original numeric representation without mistaking that internal map for a
// user-supplied JSON object.
struct NoRepeatedKeys;

impl<'de> Deserialize<'de> for NoRepeatedKeys {
    fn deserialize<D: serde::Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        d.deserialize_any(KeyVisitor)
    }
}

struct KeyVisitor;
impl<'de> Visitor<'de> for KeyVisitor {
    type Value = NoRepeatedKeys;

    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("a JSON value without duplicate object keys")
    }

    fn visit_bool<E: de::Error>(self, _: bool) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_i64<E: de::Error>(self, _: i64) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_u64<E: de::Error>(self, _: u64) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_f64<E: de::Error>(self, _: f64) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_str<E: de::Error>(self, _: &str) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_string<E: de::Error>(self, _: String) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_none<E: de::Error>(self) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
        Ok(NoRepeatedKeys)
    }
    fn visit_some<D: serde::Deserializer<'de>>(self, d: D) -> Result<Self::Value, D::Error> {
        NoRepeatedKeys::deserialize(d)
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut a: A) -> Result<Self::Value, A::Error> {
        while a.next_element::<NoRepeatedKeys>()?.is_some() {}
        Ok(NoRepeatedKeys)
    }
    fn visit_map<A: MapAccess<'de>>(self, mut a: A) -> Result<Self::Value, A::Error> {
        let mut seen = BTreeSet::new();
        while let Some(key) = a.next_key::<String>()? {
            if !seen.insert(key) {
                return Err(A::Error::custom("duplicate JSON object key"));
            }
            a.next_value::<NoRepeatedKeys>()?;
        }
        Ok(NoRepeatedKeys)
    }
}

fn validate_complexity(value: &Value, depth: usize, remaining: &mut usize) -> Result<(), Error> {
    if depth > 32 || *remaining == 0 {
        return Err(Error::InvalidArguments);
    }
    *remaining -= 1;
    match value {
        Value::Object(entries) => {
            for value in entries.values() {
                validate_complexity(value, depth + 1, remaining)?;
            }
        }
        Value::Array(elements) => {
            for value in elements {
                validate_complexity(value, depth + 1, remaining)?;
            }
        }
        _ => (),
    }
    Ok(())
}

/// Match Go's sorted-map JSON representation, including HTML-safe string escaping.
pub fn canonical_arguments(raw: &[u8]) -> Result<Vec<u8>, Error> {
    if raw.len() > 1 << 20 {
        return Err(Error::InvalidArguments);
    }
    let raw = if raw.is_empty() { b"{}".as_slice() } else { raw };
    let mut validator = serde_json::Deserializer::from_slice(raw);
    NoRepeatedKeys::deserialize(&mut validator).map_err(|_| Error::InvalidArguments)?;
    validator.end().map_err(|_| Error::InvalidArguments)?;

    let value: Value = serde_json::from_slice(raw).map_err(|_| Error::InvalidArguments)?;
    if !value.is_object() {
        return Err(Error::InvalidArguments);
    }
    validate_complexity(&value, 0, &mut 10_000)?;
    let encoded = serde_json::to_string(&value).map_err(|_| Error::InvalidArguments)?;
    let escaped = encoded
        .replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029");
    Ok(escaped.into_bytes())
}

pub fn argument_digest(raw: &[u8]) -> Result<[u8; 32], Error> {
    let canonical = canonical_arguments(raw)?;
    Ok(Sha256::digest(canonical).into())
}

pub fn argument_digest_hex(raw: &[u8]) -> Result<String, Error> {
    Ok(hex::encode(argument_digest(raw)?))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sorts_nested_keys() {
        let output = canonical_arguments(br#"{"z":1,"a":{"y":true,"x":null}}"#).unwrap();
        assert_eq!(output, br#"{"a":{"x":null,"y":true},"z":1}"#);
    }

    #[test]
    fn preserves_arbitrary_precision_numbers_and_exponents() {
        let output = canonical_arguments(
            br#"{"v":1234567890123456789012345678901234567890,"x":1.234e-12}"#,
        ).unwrap();
        assert_eq!(
            output,
            br#"{"v":1234567890123456789012345678901234567890,"x":1.234e-12}"#
        );
    }

    #[test]
    fn rejects_duplicate_nested_keys_and_trailing_objects() {
        for raw in [
            br#"{"a":1,"a":2}"#.as_slice(),
            br#"{"x":{"k":1,"k":2}}"#,
            br#"[1,2,3]"#,
            br#"{"x":1}{"y":2}"#,
        ] {
            assert!(canonical_arguments(raw).is_err());
        }
    }

    #[test]
    fn matches_go_encoding_json_number_and_string_vectors() {
        let cases: &[(&[u8], &[u8])] = &[
            (
                br#"{"numbers":[1.0,1e+30,-0,0.0000001],"slash":"a/b","quotes":"\\\""}"#,
                br#"{"numbers":[1.0,1e+30,-0,0.0000001],"quotes":"\\\"","slash":"a/b"}"#,
            ),
            (
                br#"{"unicode":"\u2028\u2029\u003c"}"#,
                br#"{"unicode":"\u2028\u2029\u003c"}"#,
            ),
        ];
        for (input, expected) in cases {
            assert_eq!(canonical_arguments(input).unwrap().as_slice(), *expected);
        }
    }

    #[test]
    fn uses_go_html_escaping() {
        assert_eq!(
            canonical_arguments(br#"{"operator":"<owner>&"}"#).unwrap(),
            br#"{"operator":"\u003cowner\u003e\u0026"}"#
        );
    }
}
