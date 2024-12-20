(* autogenerated from github.com/goose-lang/goose/testdata/examples/externalglobals *)
From New.golang Require Import defn.
From New.code Require github_com.goose_lang.goose.testdata.examples.unittest.

From New Require Import disk_prelude.

(* go: g.go:7:6 *)
Definition f : val :=
  rec: "f" <> :=
    exception_do (let: "$r0" := #(W64 11) in
    do:  ((globals.get unittest.GlobalX #()) <-[uint64T] "$r0")).

Definition pkg_name' : go_string := "github.com/goose-lang/goose/testdata/examples/externalglobals".

Definition define' : val :=
  rec: "define'" <> :=
    exception_do (do:  #()).

Definition initialize' : val :=
  rec: "initialize'" <> :=
    globals.package_init pkg_name' (λ: <>,
      exception_do (do:  unittest.initialize';;;
      do:  (define' #()))
      ).
