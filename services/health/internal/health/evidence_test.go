package health

import(
 "bytes"
 "context"
 "encoding/base64"
 "image"
 "image/color"
 "image/png"
 "testing"
)

func TestEvidenceStoresOriginalImageAndRestrictsMIME(t *testing.T){
 s:=testStore(t);ctx:=context.Background()
 picture:=image.NewRGBA(image.Rect(0,0,2,2));picture.Set(0,0,color.RGBA{255,20,30,255})
 var buf bytes.Buffer
 if err:=png.Encode(&buf,picture);err!=nil{t.Fatal(err)}
 encoded:=base64.StdEncoding.EncodeToString(buf.Bytes())
 if _,err:=s.AddEvidence(ctx,"photo.png","image/jpeg",encoded,"manual","meal");err==nil{
  t.Fatal("accepted mismatched media type")
 }
 e,err:=s.AddEvidence(ctx,"photo.png","image/png",encoded,"manual","meal")
 if err!=nil{t.Fatal(err)}
 if e.Bytes!=buf.Len(){t.Fatalf("incorrect evidence length: %+v",e)}
 got,content,err:=s.GetEvidence(ctx,e.ID,true)
 if err!=nil||got.SHA256!=e.SHA256||content!=encoded{t.Fatalf("evidence roundtrip: %+v %v",got,err)}
 list,err:=s.ListEvidence(ctx,10)
 if err!=nil||len(list)!=1{t.Fatalf("evidence list: %+v %v",list,err)}
 if _,err:=s.AddEvidence(ctx,"../secret","image/png",encoded,"manual","");err==nil{
  t.Fatal("accepted file path in name")
 }
}
