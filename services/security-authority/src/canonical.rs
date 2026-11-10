//! Canonical argument hashing and duplicate-key rejection.
use std::{collections::BTreeMap, fmt};
use serde::de::{self, MapAccess, SeqAccess, Visitor};
use serde::Deserialize;
use serde_json::{Map, Number, Value};
use sha2::{Digest, Sha256};
use crate::Error;

struct StrictValue(Value);

impl<'de> Deserialize<'de> for StrictValue {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        deserializer.deserialize_any(StrictVisitor)
    }
}

struct StrictVisitor;
impl<'de> Visitor<'de> for StrictVisitor {
    type Value = StrictValue;
    fn expecting(&self, f: &mut fmt::Formatter) -> fmt::Result {
        f.write_str("a JSON value without repeated object keys")
    }
    fn visit_bool<E: de::Error>(self, v: bool) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::Bool(v)))
    }
    fn visit_i64<E: de::Error>(self, v: i64) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::Number(Number::from(v))))
    }
    fn visit_u64<E: de::Error>(self, v: u64) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::Number(Number::from(v))))
    }
    fn visit_f64<E: de::Error>(self, v: f64) -> Result<Self::Value, E> {
        Number::from_f64(v)
            .map(|n| StrictValue(Value::Number(n)))
            .ok_or_else(|| E::custom("non-finite JSON number"))
    }
    fn visit_str<E: de::Error>(self, v: &str) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::String(v.to_owned())))
    }
    fn visit_string<E: de::Error>(self, v: String) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::String(v)))
    }
    fn visit_none<E: de::Error>(self) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::Null))
    }
    fn visit_unit<E: de::Error>(self) -> Result<Self::Value, E> {
        Ok(StrictValue(Value::Null))
    }
    fn visit_some<D: serde::Deserializer<'de>>(self, d: D) -> Result<Self::Value, D::Error> {
        StrictValue::deserialize(d)
    }
    fn visit_seq<A: SeqAccess<'de>>(self, mut a: A) -> Result<Self::Value, A::Error> {
        let mut elements = Vec::new();
        while let Some(value) = a.next_element::<StrictValue>()? {
            elements.push(value.0);
        }
        Ok(StrictValue(Value::Array(elements)))
    }
    fn visit_map<A: MapAccess<'de>>(self, mut a: A) -> Result<Self::Value, A::Error> {
        let mut entries = Map::new();
        while let Some(key) = a.next_key::<String>()? {
            if entries.contains_key(&key) {
                return Err(A::Error::custom("duplicate JSON object key"));
            }
            let value = a.next_value::<StrictValue>()?;
            entries.insert(key, value.0);
        }
        Ok(StrictValue(Value::Object(entries)))
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
    let mut parser = serde_json::Deserializer::from_slice(raw);
    let StrictValue(value) = StrictValue::deserialize(&mut parser)
        .map_err(|_| Error::InvalidArguments)?;
    parser.end().map_err(|_| Error::InvalidArguments)?;
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
    fn canonicalizes_unsorted_and_nested_arguments() {
        let encoded = canonical_arguments(br#"{"z":1,"a":{"y":true,"x":null}}"#).unwrap();
        assert_eq!(encoded, br#"{"a":{"x":null,"y":true},"z":1}"#);
    }
    #[test]
    fn rejects_duplicate_nested_keys_and_nonobjects() {
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
    fn uses_go_safe_html_escaping() {
        assert_eq!(
            canonical_arguments(br#"{"operator":"<owner>&"}"#).unwrap(),
            br#"{"operator":"\u003cowner\u003e\u0026"}"#
        );
    }
}
