(* autogenerated from github.com/goose-lang/goose/testdata/badgoose *)
From Perennial.goose_lang Require Import prelude.

Section code.
Context `{ext_ty: ext_types}.
Local Coercion Var' s: expr := Var s.

Definition mapGetCall: val :=
  rec: "mapGetCall" <> :=
    let: "handlers" := NewMap uint64T (unitT -> unitT)%ht #() in
    MapInsert "handlers" #0 (λ: <>,
      #()
      );;
    "handlers" #();;
    #().

End code.