(* autogenerated from unittest *)
From Perennial.goose_lang Require Import prelude.
From Perennial.goose_lang Require Import ffi.disk_prelude.

From Goose Require github_com.tchajed.marshal.

(* comments.go *)

(* This struct is very important.

   This is despite it being empty. *)
Module importantStruct.
  Definition S := struct.decl [
  ].
End importantStruct.

(* doSubtleThings does a number of subtle things:

   (actually, it does nothing) *)
Definition doSubtleThings: val :=
  rec: "doSubtleThings" <> :=
    #().

(* condvar.go *)

Definition condvarWrapping: val :=
  rec: "condvarWrapping" <> :=
    let: "mu" := ref (zero_val lockRefT) in
    "mu" <-[lockRefT] lock.new #();;
    let: "cond1" := lock.newCond (![lockRefT] "mu") in
    "mu" <-[lockRefT] lock.new #();;
    lock.condWait "cond1".

(* const.go *)

Definition GlobalConstant : expr := #(str"foo").

(* an untyped string *)
Definition UntypedStringConstant : expr := #(str"bar").

Definition TypedInt : expr := #32.

Definition ConstWithArith : expr := #4 + #3 * TypedInt.

Definition TypedInt32 : expr := #(U32 3).

Definition DivisionInConst : expr := (#4096 - #8) `quot` #8.

(* 517 *)
Definition ModInConst : expr := #513 + #12 `rem` #8.

(* 5 *)
Definition ModInConstParens : expr := (#513 + #12) `rem` #8.

(* control_flow.go *)

Definition conditionalReturn: val :=
  rec: "conditionalReturn" "x" :=
    (if: "x"
    then #0
    else #1).

Definition alwaysReturn: val :=
  rec: "alwaysReturn" "x" :=
    (if: "x"
    then #0
    else #1).

Definition earlyReturn: val :=
  rec: "earlyReturn" "x" :=
    (if: "x"
    then #()
    else #()).

Definition conditionalAssign: val :=
  rec: "conditionalAssign" "x" :=
    let: "y" := ref (zero_val uint64T) in
    (if: "x"
    then "y" <-[uint64T] #1
    else "y" <-[uint64T] #2);;
    "y" <-[uint64T] ![uint64T] "y" + #1;;
    ![uint64T] "y".

(* conversions.go *)

Definition stringWrapper: ty := stringT.

Definition typedLiteral: val :=
  rec: "typedLiteral" <> :=
    #3.

Definition literalCast: val :=
  rec: "literalCast" <> :=
    let: "x" := #2 in
    "x" + #2.

Definition castInt: val :=
  rec: "castInt" "p" :=
    slice.len "p".

Definition stringToByteSlice: val :=
  rec: "stringToByteSlice" "s" :=
    let: "p" := Data.stringToBytes "s" in
    "p".

Definition byteSliceToString: val :=
  rec: "byteSliceToString" "p" :=
    let: "s" := Data.bytesToString "p" in
    "s".

Definition stringToStringWrapper: val :=
  rec: "stringToStringWrapper" "s" :=
    "s".

Definition stringWrapperToString: val :=
  rec: "stringWrapperToString" "s" :=
    "s".

(* copy.go *)

Definition testCopySimple: val :=
  rec: "testCopySimple" <> :=
    let: "x" := NewSlice byteT #10 in
    SliceSet byteT "x" #3 (#(U8 1));;
    let: "y" := NewSlice byteT #10 in
    SliceCopy byteT "y" "x";;
    (SliceGet byteT "y" #3 = #(U8 1)).

Definition testCopyDifferentLengths: val :=
  rec: "testCopyDifferentLengths" <> :=
    let: "x" := NewSlice byteT #15 in
    SliceSet byteT "x" #3 (#(U8 1));;
    SliceSet byteT "x" #12 (#(U8 2));;
    let: "y" := NewSlice byteT #10 in
    let: "n" := SliceCopy byteT "y" "x" in
    ("n" = #10) && (SliceGet byteT "y" #3 = #(U8 1)).

(* data_structures.go *)

Definition atomicCreateStub: val :=
  rec: "atomicCreateStub" "dir" "fname" "data" :=
    #().

Definition useSlice: val :=
  rec: "useSlice" <> :=
    let: "s" := NewSlice byteT #1 in
    let: "s1" := SliceAppendSlice byteT "s" "s" in
    atomicCreateStub #(str"dir") #(str"file") "s1".

Definition useSliceIndexing: val :=
  rec: "useSliceIndexing" <> :=
    let: "s" := NewSlice uint64T #2 in
    SliceSet uint64T "s" #1 #2;;
    let: "x" := SliceGet uint64T "s" #0 in
    "x".

Definition useMap: val :=
  rec: "useMap" <> :=
    let: "m" := NewMap (slice.T byteT) in
    MapInsert "m" #1 slice.nil;;
    let: ("x", "ok") := MapGet "m" #2 in
    (if: "ok"
    then #()
    else MapInsert "m" #3 "x").

Definition usePtr: val :=
  rec: "usePtr" <> :=
    let: "p" := ref (zero_val uint64T) in
    "p" <-[refT uint64T] #1;;
    let: "x" := ![uint64T] "p" in
    "p" <-[refT uint64T] "x".

Definition iterMapKeysAndValues: val :=
  rec: "iterMapKeysAndValues" "m" :=
    let: "sumPtr" := ref (zero_val uint64T) in
    MapIter "m" (λ: "k" "v",
      let: "sum" := ![uint64T] "sumPtr" in
      "sumPtr" <-[refT uint64T] "sum" + "k" + "v");;
    let: "sum" := ![uint64T] "sumPtr" in
    "sum".

Definition iterMapKeys: val :=
  rec: "iterMapKeys" "m" :=
    let: "keysSlice" := NewSlice uint64T #0 in
    let: "keysRef" := ref (zero_val (slice.T uint64T)) in
    "keysRef" <-[refT (slice.T uint64T)] "keysSlice";;
    MapIter "m" (λ: "k" <>,
      let: "keys" := ![slice.T uint64T] "keysRef" in
      let: "newKeys" := SliceAppend uint64T "keys" "k" in
      "keysRef" <-[refT (slice.T uint64T)] "newKeys");;
    let: "keys" := ![slice.T uint64T] "keysRef" in
    "keys".

Definition getRandom: val :=
  rec: "getRandom" <> :=
    let: "r" := Data.randomUint64 #() in
    "r".

(* disk.go *)

Module diskWrapper.
  Definition S := struct.decl [
    "d" :: disk.Disk
  ].
End diskWrapper.

Definition diskArgument: val :=
  rec: "diskArgument" "d" :=
    let: "b" := disk.Read #0 in
    disk.Write #1 "b".

(* empty_functions.go *)

Definition empty: val :=
  rec: "empty" <> :=
    #().

Definition emptyReturn: val :=
  rec: "emptyReturn" <> :=
    #().

(* encoding.go *)

Module Enc.
  Definition S := struct.decl [
    "p" :: slice.T byteT
  ].
End Enc.

Definition Enc__consume: val :=
  rec: "Enc__consume" "e" "n" :=
    let: "b" := SliceTake (struct.loadF Enc.S "p" "e") "n" in
    struct.storeF Enc.S "p" "e" (SliceSkip byteT (struct.loadF Enc.S "p" "e") "n");;
    "b".

Definition Enc__UInt64: val :=
  rec: "Enc__UInt64" "e" "x" :=
    UInt64Put (Enc__consume "e" #8) "x".

Definition Enc__UInt32: val :=
  rec: "Enc__UInt32" "e" "x" :=
    UInt32Put (Enc__consume "e" #4) "x".

Module Dec.
  Definition S := struct.decl [
    "p" :: slice.T byteT
  ].
End Dec.

Definition Dec__consume: val :=
  rec: "Dec__consume" "d" "n" :=
    let: "b" := SliceTake (struct.loadF Dec.S "p" "d") "n" in
    struct.storeF Dec.S "p" "d" (SliceSkip byteT (struct.loadF Dec.S "p" "d") "n");;
    "b".

Definition Dec__UInt64: val :=
  rec: "Dec__UInt64" "d" :=
    UInt64Get (Dec__consume "d" #8).

Definition Dec__UInt32: val :=
  rec: "Dec__UInt32" "d" :=
    UInt32Get (Dec__consume "d" #4).

(* ints.go *)

Definition useInts: val :=
  rec: "useInts" "x" "y" :=
    let: "z" := ref (zero_val uint64T) in
    "z" <-[uint64T] to_u64 "y";;
    "z" <-[uint64T] ![uint64T] "z" + #1;;
    let: "y2" := ref (zero_val uint32T) in
    "y2" <-[uint32T] "y" + #(U32 3);;
    (![uint64T] "z", ![uint32T] "y2").

Definition u32: ty := uint32T.

Definition also_u32: ty := u32.

Definition ConstWithAbbrevType : expr := #(U32 3).

(* literals.go *)

Module allTheLiterals.
  Definition S := struct.decl [
    "int" :: uint64T;
    "s" :: stringT;
    "b" :: boolT
  ].
End allTheLiterals.

Definition normalLiterals: val :=
  rec: "normalLiterals" <> :=
    struct.mk allTheLiterals.S [
      "int" ::= #0;
      "s" ::= #(str"foo");
      "b" ::= #true
    ].

Definition specialLiterals: val :=
  rec: "specialLiterals" <> :=
    struct.mk allTheLiterals.S [
      "int" ::= #4096;
      "s" ::= #(str"");
      "b" ::= #false
    ].

Definition oddLiterals: val :=
  rec: "oddLiterals" <> :=
    struct.mk allTheLiterals.S [
      "int" ::= #5;
      "s" ::= #(str"backquote string");
      "b" ::= #false
    ].

(* locks.go *)

Definition useLocks: val :=
  rec: "useLocks" <> :=
    let: "m" := lock.new #() in
    lock.acquire "m";;
    lock.release "m".

Definition useCondVar: val :=
  rec: "useCondVar" <> :=
    let: "m" := lock.new #() in
    let: "c" := lock.newCond "m" in
    lock.acquire "m";;
    lock.condSignal "c";;
    lock.condWait "c";;
    lock.release "m".

Module hasCondVar.
  Definition S := struct.decl [
    "cond" :: condvarRefT
  ].
End hasCondVar.

(* log_debugging.go *)

Definition ToBeDebugged: val :=
  rec: "ToBeDebugged" "x" :=
    (* log.Println("starting function") *)
    (* log.Printf("called with %d", x) *)
    (* log.Println("ending function") *)
    "x".

Definition DoNothing: val :=
  rec: "DoNothing" <> :=
    (* log.Println("doing nothing") *)
    #().

(* loops.go *)

(* DoSomething is an impure function *)
Definition DoSomething: val :=
  rec: "DoSomething" "s" :=
    #().

Definition standardForLoop: val :=
  rec: "standardForLoop" "s" :=
    let: "sumPtr" := ref (zero_val uint64T) in
    let: "i" := ref #0 in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      (if: ![uint64T] "i" < slice.len "s"
      then
        let: "sum" := ![uint64T] "sumPtr" in
        let: "x" := SliceGet uint64T "s" (![uint64T] "i") in
        "sumPtr" <-[refT uint64T] "sum" + "x";;
        "i" <-[uint64T] ![uint64T] "i" + #1;;
        Continue
      else Break));;
    let: "sum" := ![uint64T] "sumPtr" in
    "sum".

Definition conditionalInLoop: val :=
  rec: "conditionalInLoop" <> :=
    let: "i" := ref #0 in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      (if: ![uint64T] "i" < #3
      then
        DoSomething (#(str"i is small"));;
        #()
      else #());;
      (if: ![uint64T] "i" > #5
      then Break
      else
        "i" <-[uint64T] ![uint64T] "i" + #1;;
        Continue)).

Definition ImplicitLoopContinue: val :=
  rec: "ImplicitLoopContinue" <> :=
    let: "i" := ref #0 in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      (if: ![uint64T] "i" < #4
      then "i" <-[uint64T] #0
      else #());;
      Continue).

Definition nestedLoops: val :=
  rec: "nestedLoops" <> :=
    let: "i" := ref #0 in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      let: "j" := ref #0 in
      (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
        (if: #true
        then Break
        else
          "j" <-[uint64T] ![uint64T] "j" + #1;;
          Continue));;
      "i" <-[uint64T] ![uint64T] "i" + #1;;
      Continue).

Definition nestedGoStyleLoops: val :=
  rec: "nestedGoStyleLoops" <> :=
    let: "i" := ref #0 in
    (for: (λ: <>, ![uint64T] "i" < #10); (λ: <>, "i" <-[uint64T] ![uint64T] "i" + #1) := λ: <>,
      let: "j" := ref #0 in
      (for: (λ: <>, ![uint64T] "j" < ![uint64T] "i"); (λ: <>, "j" <-[uint64T] ![uint64T] "j" + #1) := λ: <>,
        (if: #true
        then Break
        else Continue));;
      Continue).

Definition sumSlice: val :=
  rec: "sumSlice" "xs" :=
    let: "sum" := ref (zero_val uint64T) in
    ForSlice uint64T <> "x" "xs"
      ("sum" <-[uint64T] ![uint64T] "sum" + "x");;
    ![uint64T] "sum".

Definition breakFromLoop: val :=
  rec: "breakFromLoop" <> :=
    Skip;;
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      (if: #true
      then Break
      else Continue)).

(* maps.go *)

Definition clearMap: val :=
  rec: "clearMap" "m" :=
    MapClear "m".

Definition IterateMapKeys: val :=
  rec: "IterateMapKeys" "m" "sum" :=
    MapIter "m" (λ: "k" <>,
      let: "oldSum" := ![uint64T] "sum" in
      "sum" <-[refT uint64T] "oldSum" + "k").

Definition MapSize: val :=
  rec: "MapSize" "m" :=
    MapLen "m".

Definition IntWrapper: ty := uint64T.

Definition MapWrapper: ty := mapT boolT.

Definition MapTypeAliases: val :=
  rec: "MapTypeAliases" "m1" "m2" :=
    MapInsert "m1" #4 (Fst (MapGet "m2" #0)).

(* multiple.go *)

Definition returnTwo: val :=
  rec: "returnTwo" "p" :=
    (#0, #0).

Definition returnTwoWrapper: val :=
  rec: "returnTwoWrapper" "data" :=
    let: ("a", "b") := returnTwo "data" in
    ("a", "b").

Definition multipleVar: val :=
  rec: "multipleVar" "x" "y" :=
    #().

(* nil.go *)

Definition AssignNilSlice: val :=
  rec: "AssignNilSlice" <> :=
    let: "s" := NewSlice (slice.T byteT) #4 in
    SliceSet (slice.T byteT) "s" #2 slice.nil.

Definition AssignNilPointer: val :=
  rec: "AssignNilPointer" <> :=
    let: "s" := NewSlice (refT uint64T) #4 in
    SliceSet (refT uint64T) "s" #2 slice.nil.

Definition CompareSliceToNil: val :=
  rec: "CompareSliceToNil" <> :=
    let: "s" := NewSlice byteT #0 in
    "s" ≠ slice.nil.

Definition ComparePointerToNil: val :=
  rec: "ComparePointerToNil" <> :=
    let: "s" := ref (zero_val uint64T) in
    "s" ≠ slice.nil.

(* operators.go *)

Definition LogicalOperators: val :=
  rec: "LogicalOperators" "b1" "b2" :=
    "b1" && "b2" || "b1" && ~ #false.

Definition LogicalAndEqualityOperators: val :=
  rec: "LogicalAndEqualityOperators" "b1" "x" :=
    ("x" = #3) && ("b1" = #true).

Definition ArithmeticShifts: val :=
  rec: "ArithmeticShifts" "x" "y" :=
    to_u64 ("x" ≪ #3) + "y" ≪ to_u64 "x" + "y" ≪ #1.

Definition BitwiseOps: val :=
  rec: "BitwiseOps" "x" "y" :=
    to_u64 "x" ∥ (to_u64 (to_u32 "y") & #43).

Definition Comparison: val :=
  rec: "Comparison" "x" "y" :=
    (if: "x" < "y"
    then #true
    else
      (if: ("x" = "y")
      then #true
      else
        (if: "x" ≠ "y"
        then #true
        else
          (if: "x" > "y"
          then #true
          else
            (if: "x" + #1 > "y" - #2
            then #true
            else #false))))).

Definition AssignOps: val :=
  rec: "AssignOps" <> :=
    let: "x" := ref (zero_val uint64T) in
    "x" <-[uint64T] ![uint64T] "x" + #3;;
    "x" <-[uint64T] ![uint64T] "x" - #3;;
    "x" <-[uint64T] ![uint64T] "x" + #1;;
    "x" <-[uint64T] ![uint64T] "x" - #1.

(* package.go *)

Module wrapExternalStruct.
  Definition S := struct.decl [
    "e" :: struct.t marshal.Enc.S;
    "d" :: struct.t marshal.Dec.S
  ].
End wrapExternalStruct.

Definition wrapExternalStruct__moveUint64: val :=
  rec: "wrapExternalStruct__moveUint64" "w" :=
    marshal.Enc__PutInt (struct.get wrapExternalStruct.S "e" "w") (marshal.Dec__GetInt (struct.get wrapExternalStruct.S "d" "w")).

(* panic.go *)

Definition PanicAtTheDisco: val :=
  rec: "PanicAtTheDisco" <> :=
    Panic "disco".

(* reassign.go *)

Module composite.
  Definition S := struct.decl [
    "a" :: uint64T;
    "b" :: uint64T
  ].
End composite.

Definition ReassignVars: val :=
  rec: "ReassignVars" <> :=
    let: "x" := ref (zero_val uint64T) in
    let: "y" := #0 in
    "x" <-[uint64T] #3;;
    let: "z" := ref (struct.mk composite.S [
      "a" ::= ![uint64T] "x";
      "b" ::= "y"
    ]) in
    "z" <-[struct.t composite.S] struct.mk composite.S [
      "a" ::= "y";
      "b" ::= ![uint64T] "x"
    ];;
    "x" <-[uint64T] struct.get composite.S "a" (![struct.t composite.S] "z").

(* replicated_disk.go *)

Module Block.
  Definition S := struct.decl [
    "Value" :: uint64T
  ].
End Block.

Definition Disk1 : expr := #0.

Definition Disk2 : expr := #0.

Definition DiskSize : expr := #1000.

(* TwoDiskWrite is a dummy function to represent the base layer's disk write *)
Definition TwoDiskWrite: val :=
  rec: "TwoDiskWrite" "diskId" "a" "v" :=
    #true.

(* TwoDiskRead is a dummy function to represent the base layer's disk read *)
Definition TwoDiskRead: val :=
  rec: "TwoDiskRead" "diskId" "a" :=
    (struct.mk Block.S [
       "Value" ::= #0
     ], #true).

(* TwoDiskLock is a dummy function to represent locking an address in the
   base layer *)
Definition TwoDiskLock: val :=
  rec: "TwoDiskLock" "a" :=
    #().

(* TwoDiskUnlock is a dummy function to represent unlocking an address in the
   base layer *)
Definition TwoDiskUnlock: val :=
  rec: "TwoDiskUnlock" "a" :=
    #().

Definition ReplicatedDiskRead: val :=
  rec: "ReplicatedDiskRead" "a" :=
    TwoDiskLock "a";;
    let: ("v", "ok") := TwoDiskRead Disk1 "a" in
    (if: "ok"
    then
      TwoDiskUnlock "a";;
      "v"
    else
      let: ("v2", <>) := TwoDiskRead Disk2 "a" in
      TwoDiskUnlock "a";;
      "v2").

Definition ReplicatedDiskWrite: val :=
  rec: "ReplicatedDiskWrite" "a" "v" :=
    TwoDiskLock "a";;
    TwoDiskWrite Disk1 "a" "v";;
    TwoDiskWrite Disk2 "a" "v";;
    TwoDiskUnlock "a".

Definition ReplicatedDiskRecover: val :=
  rec: "ReplicatedDiskRecover" <> :=
    let: "a" := ref #0 in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      (if: ![uint64T] "a" > DiskSize
      then Break
      else
        let: ("v", "ok") := TwoDiskRead Disk1 (![uint64T] "a") in
        (if: "ok"
        then
          TwoDiskWrite Disk2 (![uint64T] "a") "v";;
          #()
        else #());;
        "a" <-[uint64T] ![uint64T] "a" + #1;;
        Continue)).

(* slices.go *)

Definition SliceAlias: ty := slice.T boolT.

Definition sliceOps: val :=
  rec: "sliceOps" <> :=
    let: "x" := NewSlice uint64T #10 in
    let: "v1" := SliceGet uint64T "x" #2 in
    let: "v2" := SliceSubslice uint64T "x" #2 #3 in
    let: "v3" := SliceTake "x" #3 in
    let: "v4" := SliceRef "x" #2 in
    "v1" + SliceGet uint64T "v2" #0 + SliceGet uint64T "v3" #1 + ![uint64T] "v4".

Definition makeSingletonSlice: val :=
  rec: "makeSingletonSlice" "x" :=
    SliceSingleton "x".

Module thing.
  Definition S := struct.decl [
    "x" :: uint64T
  ].
End thing.

Module sliceOfThings.
  Definition S := struct.decl [
    "things" :: slice.T (struct.t thing.S)
  ].
End sliceOfThings.

Definition sliceOfThings__getThingRef: val :=
  rec: "sliceOfThings__getThingRef" "ts" "i" :=
    SliceRef (struct.get sliceOfThings.S "things" "ts") "i".

Definition makeAlias: val :=
  rec: "makeAlias" <> :=
    NewSlice boolT #10.

(* spawn.go *)

(* Skip is a placeholder for some impure code *)
Definition Skip: val :=
  rec: "Skip" <> :=
    #().

Definition simpleSpawn: val :=
  rec: "simpleSpawn" <> :=
    let: "l" := lock.new #() in
    let: "v" := ref (zero_val uint64T) in
    Fork (lock.acquire "l";;
          let: "x" := ![uint64T] "v" in
          (if: "x" > #0
          then
            Skip #();;
            #()
          else #());;
          lock.release "l");;
    lock.acquire "l";;
    "v" <-[refT uint64T] #1;;
    lock.release "l".

Definition threadCode: val :=
  rec: "threadCode" "tid" :=
    #().

Definition loopSpawn: val :=
  rec: "loopSpawn" <> :=
    let: "i" := ref #0 in
    (for: (λ: <>, ![uint64T] "i" < #10); (λ: <>, "i" <-[uint64T] ![uint64T] "i" + #1) := λ: <>,
      let: "i" := ![uint64T] "i" in
      Fork (threadCode "i");;
      Continue);;
    let: "dummy" := ref #true in
    (for: (λ: <>, #true); (λ: <>, Skip) := λ: <>,
      "dummy" <-[boolT] ~ (![boolT] "dummy");;
      Continue).

(* strings.go *)

Definition stringAppend: val :=
  rec: "stringAppend" "s" "x" :=
    #(str"prefix ") + "s" + #(str" ") + uint64_to_string "x".

Definition stringLength: val :=
  rec: "stringLength" "s" :=
    strLen "s".

(* struct_method.go *)

Module Point.
  Definition S := struct.decl [
    "x" :: uint64T;
    "y" :: uint64T
  ].
End Point.

Definition Point__Add: val :=
  rec: "Point__Add" "c" "z" :=
    struct.get Point.S "x" "c" + struct.get Point.S "y" "c" + "z".

Definition Point__GetField: val :=
  rec: "Point__GetField" "c" :=
    let: "x" := struct.get Point.S "x" "c" in
    let: "y" := struct.get Point.S "y" "c" in
    "x" + "y".

Definition UseAdd: val :=
  rec: "UseAdd" <> :=
    let: "c" := struct.mk Point.S [
      "x" ::= #2;
      "y" ::= #3
    ] in
    let: "r" := Point__Add "c" #4 in
    "r".

Definition UseAddWithLiteral: val :=
  rec: "UseAddWithLiteral" <> :=
    let: "r" := Point__Add (struct.mk Point.S [
      "x" ::= #2;
      "y" ::= #3
    ]) #4 in
    "r".

(* struct_pointers.go *)

Module TwoInts.
  Definition S := struct.decl [
    "x" :: uint64T;
    "y" :: uint64T
  ].
End TwoInts.

Module S.
  Definition S := struct.decl [
    "a" :: uint64T;
    "b" :: struct.t TwoInts.S;
    "c" :: boolT
  ].
End S.

Definition NewS: val :=
  rec: "NewS" <> :=
    struct.new S.S [
      "a" ::= #2;
      "b" ::= struct.mk TwoInts.S [
        "x" ::= #1;
        "y" ::= #2
      ];
      "c" ::= #true
    ].

Definition S__readA: val :=
  rec: "S__readA" "s" :=
    struct.loadF S.S "a" "s".

Definition S__readB: val :=
  rec: "S__readB" "s" :=
    struct.loadF S.S "b" "s".

Definition S__readBVal: val :=
  rec: "S__readBVal" "s" :=
    struct.get S.S "b" "s".

Definition S__writeB: val :=
  rec: "S__writeB" "s" "two" :=
    struct.storeF S.S "b" "s" "two".

Definition S__negateC: val :=
  rec: "S__negateC" "s" :=
    struct.storeF S.S "c" "s" (~ (struct.loadF S.S "c" "s")).

Definition S__refC: val :=
  rec: "S__refC" "s" :=
    struct.fieldRef S.S "c" "s".

Definition localSRef: val :=
  rec: "localSRef" <> :=
    let: "s" := ref (zero_val (struct.t S.S)) in
    struct.fieldRef S.S "b" "s".

Definition setField: val :=
  rec: "setField" <> :=
    let: "s" := ref (zero_val (struct.t S.S)) in
    struct.storeF S.S "a" "s" #0;;
    struct.storeF S.S "c" "s" #true;;
    ![struct.t S.S] "s".

(* synchronization.go *)

(* DoSomeLocking uses the entire lock API *)
Definition DoSomeLocking: val :=
  rec: "DoSomeLocking" "l" :=
    lock.acquire "l";;
    lock.release "l".

Definition makeLock: val :=
  rec: "makeLock" <> :=
    let: "l" := lock.new #() in
    DoSomeLocking "l".

(* type_alias.go *)

Definition u64: ty := uint64T.

Definition Timestamp: ty := uint64T.

Definition UseTypeAbbrev: ty := u64.

Definition UseNamedType: ty := Timestamp.

Definition convertToAlias: val :=
  rec: "convertToAlias" <> :=
    let: "x" := #2 in
    "x".
