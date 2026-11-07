// Package canonicaljson validates and canonicalizes tool argument objects.
// It preserves arbitrary-precision JSON numbers and rejects duplicate object keys.
package canonicaljson

import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
)

const maxBytes = 1 << 20
const maxDepth = 32
const maxNodes = 10000

// Object returns stable JSON and a parsed object for approval previews.
// Canonical encoding must be used at both authorization and execution.
func Object(raw json.RawMessage) ([]byte, map[string]any, error) {
    if len(raw) == 0 { raw = json.RawMessage("{}") }
    if len(raw) > maxBytes { return nil,nil,errors.New("tool arguments exceed size limit") }
    decoder := json.NewDecoder(bytes.NewReader(raw))
    decoder.UseNumber()
    remaining := maxNodes
    value,err:=parseValue(decoder,0,&remaining)
    if err!=nil{return nil,nil,err}
    object,ok:=value.(map[string]any)
    if !ok{return nil,nil,errors.New("tool arguments must be an object")}
    if _,err:=decoder.Token();err!=io.EOF{
        return nil,nil,errors.New("unexpected content after tool arguments")
    }
    encoded,err:=json.Marshal(object)
    if err!=nil{return nil,nil,errors.New("cannot encode tool arguments")}
    return encoded,object,nil
}

func parseValue(dec *json.Decoder,depth int,remaining *int)(any,error){
    if depth>maxDepth{return nil,errors.New("tool arguments exceed maximum nesting")}
    *remaining--
    if *remaining<0{return nil,errors.New("tool arguments exceed maximum complexity")}
    token,err:=dec.Token()
    if err!=nil{return nil,errors.New("invalid JSON in tool arguments")}
    switch value:=token.(type){
    case json.Delim:
        switch value {
        case '{':
            object:=make(map[string]any)
            for dec.More(){
                field,err:=dec.Token()
                if err!=nil{return nil,errors.New("invalid object key")}
                key,ok:=field.(string)
                if !ok{return nil,errors.New("invalid object key")}
                if _,exists:=object[key];exists{return nil,errors.New("duplicate JSON object key")}
                entry,err:=parseValue(dec,depth+1,remaining)
                if err!=nil{return nil,err}
                object[key]=entry
            }
            if end,err:=dec.Token();err!=nil||end!=json.Delim('}'){
                return nil,errors.New("unterminated JSON object")
            }
            return object,nil
        case '[':
            array:=make([]any,0)
            for dec.More(){
                entry,err:=parseValue(dec,depth+1,remaining)
                if err!=nil{return nil,err}
                array=append(array,entry)
            }
            if end,err:=dec.Token();err!=nil||end!=json.Delim(']'){
                return nil,errors.New("unterminated JSON array")
            }
            return array,nil
        }
        return nil,errors.New("unexpected JSON delimiter")
    case string,bool,json.Number,nil:
        return value,nil
    default:
        return nil,errors.New("unsupported JSON value")
    }
}
