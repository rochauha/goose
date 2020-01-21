(* autogenerated from append_log *)
From Perennial.go_lang Require Import prelude.

(* disk FFI *)
From Perennial.go_lang Require Import ffi.disk_prelude.

(* Append-only, sequential, crash-safe log.

   The main interesting feature is that the log supports multi-block atomic
   appends, which are implemented by atomically updating an on-disk header with
   the number of valid blocks in the log. *)

Module Enc.
  (* Enc is a stateful encoder for a single disk block. *)
  Definition S := struct.decl [
    "b" :: disk.blockT;
    "off" :: refT uint64T
  ].
  Definition T: ty := struct.t S.
  Definition Ptr: ty := struct.ptrT S.
  Section fields.
    Context `{ext_ty: ext_types}.
    Definition get := struct.get S.
  End fields.
End Enc.

Definition NewEnc: val :=
  λ: <>,
    struct.mk Enc.S [
      "b" ::= NewSlice byteT disk.BlockSize;
      "off" ::= ref (zero_val uint64T)
    ].
Theorem NewEnc_t: ⊢ NewEnc : (unitT -> Enc.T).
Proof. typecheck. Qed.
Hint Resolve NewEnc_t : types.

Definition Enc__PutInt: val :=
  λ: "enc" "x",
    let: "off" := !(Enc.get "off" "enc") in
    UInt64Put (SliceSkip (Enc.get "b" "enc") "off") "x";;
    Enc.get "off" "enc" <- !(Enc.get "off" "enc") + #8.
Theorem Enc__PutInt_t: ⊢ Enc__PutInt : (Enc.T -> uint64T -> unitT).
Proof. typecheck. Qed.
Hint Resolve Enc__PutInt_t : types.

Definition Enc__Finish: val :=
  λ: "enc",
    Enc.get "b" "enc".
Theorem Enc__Finish_t: ⊢ Enc__Finish : (Enc.T -> disk.blockT).
Proof. typecheck. Qed.
Hint Resolve Enc__Finish_t : types.

Module Dec.
  (* Dec is a stateful decoder that returns values encoded
     sequentially in a single disk block. *)
  Definition S := struct.decl [
    "b" :: disk.blockT;
    "off" :: refT uint64T
  ].
  Definition T: ty := struct.t S.
  Definition Ptr: ty := struct.ptrT S.
  Section fields.
    Context `{ext_ty: ext_types}.
    Definition get := struct.get S.
  End fields.
End Dec.

Definition NewDec: val :=
  λ: "b",
    struct.mk Dec.S [
      "b" ::= "b";
      "off" ::= ref (zero_val uint64T)
    ].
Theorem NewDec_t: ⊢ NewDec : (disk.blockT -> Dec.T).
Proof. typecheck. Qed.
Hint Resolve NewDec_t : types.

Definition Dec__GetInt: val :=
  λ: "dec",
    let: "off" := !(Dec.get "off" "dec") in
    Dec.get "off" "dec" <- !(Dec.get "off" "dec") + #8;;
    UInt64Get (SliceSkip (Dec.get "b" "dec") "off").
Theorem Dec__GetInt_t: ⊢ Dec__GetInt : (Dec.T -> uint64T).
Proof. typecheck. Qed.
Hint Resolve Dec__GetInt_t : types.

Module Log.
  Definition S := struct.decl [
    "sz" :: uint64T;
    "diskSz" :: uint64T
  ].
  Definition T: ty := struct.t S.
  Definition Ptr: ty := struct.ptrT S.
  Section fields.
    Context `{ext_ty: ext_types}.
    Definition get := struct.get S.
  End fields.
End Log.

Definition Log__mkHdr: val :=
  λ: "log",
    let: "enc" := NewEnc #() in
    Enc__PutInt "enc" (Log.get "sz" "log");;
    Enc__PutInt "enc" (Log.get "diskSz" "log");;
    Enc__Finish "enc".
Theorem Log__mkHdr_t: ⊢ Log__mkHdr : (Log.T -> disk.blockT).
Proof. typecheck. Qed.
Hint Resolve Log__mkHdr_t : types.

Definition Log__writeHdr: val :=
  λ: "log",
    disk.Write #0 (Log__mkHdr "log").
Theorem Log__writeHdr_t: ⊢ Log__writeHdr : (Log.T -> unitT).
Proof. typecheck. Qed.
Hint Resolve Log__writeHdr_t : types.

Definition Init: val :=
  λ: "diskSz",
    (if: "diskSz" < #1
    then
      (struct.mk Log.S [
         "sz" ::= #0;
         "diskSz" ::= #0
       ], #false)
    else
      let: "log" := struct.mk Log.S [
        "sz" ::= #0;
        "diskSz" ::= "diskSz"
      ] in
      Log__writeHdr "log";;
      ("log", #true)).
Theorem Init_t: ⊢ Init : (uint64T -> (Log.T * boolT)).
Proof. typecheck. Qed.
Hint Resolve Init_t : types.

Definition Open: val :=
  λ: <>,
    let: "hdr" := disk.Read #0 in
    let: "dec" := NewDec "hdr" in
    let: "sz" := Dec__GetInt "dec" in
    let: "diskSz" := Dec__GetInt "dec" in
    struct.mk Log.S [
      "sz" ::= "sz";
      "diskSz" ::= "diskSz"
    ].
Theorem Open_t: ⊢ Open : (unitT -> Log.T).
Proof. typecheck. Qed.
Hint Resolve Open_t : types.

Definition Log__Get: val :=
  λ: "log" "i",
    let: "sz" := Log.get "sz" "log" in
    (if: "i" < "sz"
    then (disk.Read (#1 + "i"), #true)
    else (slice.nil, #false)).
Theorem Log__Get_t: ⊢ Log__Get : (Log.T -> uint64T -> (disk.blockT * boolT)).
Proof. typecheck. Qed.
Hint Resolve Log__Get_t : types.

Definition writeAll: val :=
  λ: "bks" "off",
    ForSlice "i" "bk" "bks"
      (disk.Write ("off" + "i") "bk").
Theorem writeAll_t: ⊢ writeAll : (slice.T disk.blockT -> uint64T -> unitT).
Proof. typecheck. Qed.
Hint Resolve writeAll_t : types.

Definition Log__Append: val :=
  λ: "log" "bks",
    let: "sz" := struct.loadF Log.S "sz" "log" in
    (if: slice.len "bks" ≥ struct.loadF Log.S "diskSz" "log" - #1 - "sz"
    then #false
    else
      writeAll "bks" (#1 + "sz");;
      let: "newLog" := struct.mk Log.S [
        "sz" ::= "sz" + slice.len "bks";
        "diskSz" ::= struct.loadF Log.S "diskSz" "log"
      ] in
      Log__writeHdr "newLog";;
      struct.store Log.S "log" "newLog";;
      #true).
Theorem Log__Append_t: ⊢ Log__Append : (struct.ptrT Log.S -> slice.T disk.blockT -> boolT).
Proof. typecheck. Qed.
Hint Resolve Log__Append_t : types.

Definition Log__Reset: val :=
  λ: "log",
    let: "newLog" := struct.mk Log.S [
      "sz" ::= #0;
      "diskSz" ::= struct.loadF Log.S "diskSz" "log"
    ] in
    Log__writeHdr "newLog";;
    struct.store Log.S "log" "newLog".
Theorem Log__Reset_t: ⊢ Log__Reset : (struct.ptrT Log.S -> unitT).
Proof. typecheck. Qed.
Hint Resolve Log__Reset_t : types.
