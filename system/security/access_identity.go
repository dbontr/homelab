package security

import (
 "crypto"
 "crypto/rsa"
 "crypto/sha256"
 "encoding/base64"
 "encoding/json"
 "errors"
 "math/big"
 "strings"
 "time"

 "github.com/dbontr/homelab-new/system/canonicaljson"
)

// AccessVerifier verifies signed identity assertions inside the independent
// security boundary. JWKS bytes are installed by the operator, not fetched
// through the MCP gateway.
type AccessVerifier struct {
 issuer string
 email string
 keys map[string]*rsa.PublicKey
 now func() time.Time
}

func NewAccessVerifier(issuer, ownerEmail string, jwks []byte) (*AccessVerifier,error) {
 email:=strings.ToLower(strings.TrimSpace(ownerEmail))
 if !strings.HasPrefix(issuer,"https://") || strings.ContainsAny(issuer,"?#") ||
   len(email)<3 || len(email)>320 || strings.Count(email,"@")!=1 ||
   strings.ContainsAny(email," \t\r\n\x00") {
  return nil,errors.New("invalid Access verifier configuration")
 }
 if len(jwks)==0 || len(jwks)>256<<10 {return nil,errors.New("invalid Access signing keys")}
 if _,_,err:=canonicaljson.Object(json.RawMessage(jwks));err!=nil{return nil,err}
 var doc struct{Keys []struct{
  Kty string `json:"kty"`
  Kid string `json:"kid"`
  Alg string `json:"alg"`
  Use string `json:"use"`
  N string `json:"n"`
  E string `json:"e"`
 } `json:"keys"`}
 if err:=json.Unmarshal(jwks,&doc);err!=nil||len(doc.Keys)==0||len(doc.Keys)>32{
  return nil,errors.New("invalid Access JWKS")
 }
 keys:=map[string]*rsa.PublicKey{}
 for _,k:=range doc.Keys {
  if k.Kty!="RSA" || k.Kid==""||len(k.Kid)>256||k.N==""||k.E=="" ||
   (k.Use!=""&&k.Use!="sig")||(k.Alg!=""&&k.Alg!="RS256"){
   return nil,errors.New("unsupported Access signing key")
  }
  if _,duplicate:=keys[k.Kid];duplicate{return nil,errors.New("duplicate Access signing key")}
  modulus,err1:=base64.RawURLEncoding.DecodeString(k.N)
  exponent,err2:=base64.RawURLEncoding.DecodeString(k.E)
  if err1!=nil||err2!=nil||len(modulus)<256||len(modulus)>512||len(exponent)==0||len(exponent)>4{
   return nil,errors.New("invalid Access public key")
  }
  n:=new(big.Int).SetBytes(modulus)
  e:=new(big.Int).SetBytes(exponent)
  if !e.IsInt64()||e.Int64()<3||e.Int64()>65537||e.Int64()%2==0||n.BitLen()<2048{
   return nil,errors.New("invalid Access RSA parameters")
  }
  keys[k.Kid]=&rsa.PublicKey{N:n,E:int(e.Int64())}
 }
 return &AccessVerifier{issuer:issuer,email:email,keys:keys,now:time.Now},nil
}

func (v *AccessVerifier) Verify(assertion,audience string)(string,error){
 if v==nil||len(assertion)<20||len(assertion)>16<<10||audience==""{
  return "",errors.New("Access identity required")
 }
 parts:=strings.Split(assertion,".")
 if len(parts)!=3{return "",errors.New("invalid Access assertion")}
 header,err:=base64.RawURLEncoding.DecodeString(parts[0])
 if err!=nil||len(header)>4096{return "",errors.New("invalid assertion header")}
 _,fields,err:=canonicaljson.Object(json.RawMessage(header))
 if err!=nil||fields["alg"]!="RS256"{return "",errors.New("unsupported assertion algorithm")}
 kid,ok:=fields["kid"].(string)
 if !ok||kid==""{return "",errors.New("missing assertion key identifier")}
 key:=v.keys[kid]
 if key==nil{return "",errors.New("unrecognized signing key")}
 payload,err:=base64.RawURLEncoding.DecodeString(parts[1])
 if err!=nil||len(payload)>16<<10{return "",errors.New("invalid assertion claims")}
 _,claims,err:=canonicaljson.Object(json.RawMessage(payload))
 if err!=nil{return "",errors.New("invalid assertion claims")}
 signature,err:=base64.RawURLEncoding.DecodeString(parts[2])
 if err!=nil||len(signature)!=key.Size(){return "",errors.New("invalid assertion signature")}
 digest:=sha256.Sum256([]byte(parts[0]+"."+parts[1]))
 if err:=rsa.VerifyPKCS1v15(key,crypto.SHA256,digest[:],signature);err!=nil{
  return "",errors.New("invalid Access signature")
 }
 iss,_:=claims["iss"].(string)
 email,_:=claims["email"].(string)
 sub,_:=claims["sub"].(string)
 email=strings.ToLower(strings.TrimSpace(email))
 now:=v.now().Unix()
 exp,expOK:=jwtSeconds(claims["exp"])
 iat,iatOK:=jwtSeconds(claims["iat"])
 nbf,nbfOK:=jwtSeconds(claims["nbf"])
 if _,present:=claims["nbf"];!present{nbf=iat;nbfOK=iatOK}
 if !nbfOK{return "",errors.New("invalid Access not-before claim")}
 if !expOK||!iatOK||iss!=v.issuer||email!=v.email||sub==""||len(sub)>512||
    strings.ContainsAny(sub,"\r\n\x00")||iat>now+30||nbf>now+30||exp<=now||
    exp<=iat||exp-iat>24*3600||iat<now-24*3600{
   return "",errors.New("invalid Access identity or lifetime")
 }
 audOK:=false
 switch aud:=claims["aud"].(type){
 case string:audOK=aud==audience
 case []any: for _,entry:=range aud{if s,ok:=entry.(string);ok&&s==audience{audOK=true}}
 }
 if !audOK{return "",errors.New("wrong Access audience")}
 // Requiring subject and email avoids treating a service-token assertion
 // without a human user identity as an owner login.
 return v.issuer+"#"+sub+"#"+email,nil
}

func jwtSeconds(raw any)(int64,bool){
 n,ok:=raw.(json.Number)
 if !ok{return 0,false}
 v,err:=n.Int64()
 return v,err==nil
}
