package remote

import(
 "bytes"
 "errors"
 "testing"
)

func TestInspectionOutputIsBounded(t *testing.T){
 bounded:=&boundedInspection{}
 if n,err:=bounded.Write(bytes.Repeat([]byte("a"),maxInspectionBytes));err!=nil||n!=maxInspectionBytes{t.Fatal("valid bounded output rejected",n,err)}
 if n,err:=bounded.Write([]byte("over"));n!=0||!errors.Is(err,errInspectionLimit)||!bounded.limitHit{t.Fatal("output overflow was not rejected",n,err)}
 if bounded.data.Len()!=maxInspectionBytes{t.Fatal("output exceeded bound")}
}
