package main

import(
 "strings"
 "testing"
)

func TestMechanicalRules(t *testing.T){
 cases:=[]struct{label,input,expected string}{
  {"semicolon","Open the file; save the result.","SEMICOLON"},
  {"contraction","Don't delete the repository.","CONTRACTION"},
  {"long sentence",strings.Repeat("accurate ",26)+".","SENTENCE_LENGTH"},
  {"spelling","The colour is green.","SPELLING"},
  {"passive","The file is modified.","PASSIVE_VOICE"},
 }
 for _,tc:=range cases{
  t.Run(tc.label,func(t *testing.T){
   found:=false
   for _,item:=range check(tc.input,"procedure"){if item.Code==tc.expected{found=true}}
   if !found{t.Fatalf("missing %s",tc.expected)}
  })
 }
}

func TestProtectedCodeAndParagraphLength(t *testing.T){
 code:=""+string(rune(96))+string(rune(96))+string(rune(96))+"\n"+
  "Don't use this; it is a code example.\n"+
  string(rune(96))+string(rune(96))+string(rune(96))
 if found:=check(code,"description");len(found)!=0{t.Fatal("code was checked as prose",found)}
 paragraph:="One. Two. Three. Four. Five. Six. Seven."
 found:=false
 for _,item:=range check(paragraph,"description"){if item.Code=="PARAGRAPH_LENGTH"{found=true}}
 if !found{t.Fatal("seven-sentence paragraph was permitted")}
}
