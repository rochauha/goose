(* autogenerated from github.com/tchajed/goose/internal/examples/append_log *)
From Perennial.goose_lang Require Import prelude.
From Goose Require github_com.tchajed.marshal.

From Perennial.goose_lang Require Import ffi.disk_prelude.

(* Append-only, sequential, crash-safe log.

   The main interesting feature is that the log supports multi-block atomic
   appends, which are implemented by atomically updating an on-disk header with
   the number of valid blocks in the log. *)

Definition Log := struct.decl [
  "m" :: ptrT;
  "sz" :: uint64T;
  "diskSz" :: uint64T
].

Definition Log__mkHdr: val :=
  rec: "Log__mkHdr" "log" :=
    let: "enc" := marshal.NewEnc disk.BlockSize in
    marshal.Enc__PutInt "enc" (struct.loadF Log "sz" "log");;
    marshal.Enc__PutInt "enc" (struct.loadF Log "diskSz" "log");;
    marshal.Enc__Finish "enc".

Definition Log__writeHdr: val :=
  rec: "Log__writeHdr" "log" :=
    disk.Write #0 (Log__mkHdr "log");;
    #().

Definition Init: val :=
  rec: "Init" "diskSz" :=
    (if: "diskSz" < #1
    then
      (struct.new Log [
         "m" ::= lock.new #();
         "sz" ::= #0;
         "diskSz" ::= #0
       ], #false)
    else
      let: "log" := struct.new Log [
        "m" ::= lock.new #();
        "sz" ::= #0;
        "diskSz" ::= "diskSz"
      ] in
      Log__writeHdr "log";;
      ("log", #true)).

Definition Open: val :=
  rec: "Open" <> :=
    let: "hdr" := disk.Read #0 in
    let: "dec" := marshal.NewDec "hdr" in
    let: "sz" := marshal.Dec__GetInt "dec" in
    let: "diskSz" := marshal.Dec__GetInt "dec" in
    struct.new Log [
      "m" ::= lock.new #();
      "sz" ::= "sz";
      "diskSz" ::= "diskSz"
    ].

Definition Log__get: val :=
  rec: "Log__get" "log" "i" :=
    let: "sz" := struct.loadF Log "sz" "log" in
    (if: "i" < "sz"
    then (disk.Read (#1 + "i"), #true)
    else (slice.nil, #false)).

Definition Log__Get: val :=
  rec: "Log__Get" "log" "i" :=
    lock.acquire (struct.loadF Log "m" "log");;
    let: ("v", "b") := Log__get "log" "i" in
    lock.release (struct.loadF Log "m" "log");;
    ("v", "b").

Definition writeAll: val :=
  rec: "writeAll" "bks" "off" :=
    ForSlice (slice.T byteT) "i" "bk" "bks"
      (disk.Write ("off" + "i") "bk");;
    #().

Definition Log__append: val :=
  rec: "Log__append" "log" "bks" :=
    let: "sz" := struct.loadF Log "sz" "log" in
    (if: slice.len "bks" ≥ struct.loadF Log "diskSz" "log" - #1 - "sz"
    then #false
    else
      writeAll "bks" (#1 + "sz");;
      struct.storeF Log "sz" "log" (struct.loadF Log "sz" "log" + slice.len "bks");;
      Log__writeHdr "log";;
      #true).

Definition Log__Append: val :=
  rec: "Log__Append" "log" "bks" :=
    lock.acquire (struct.loadF Log "m" "log");;
    let: "b" := Log__append "log" "bks" in
    lock.release (struct.loadF Log "m" "log");;
    "b".

Definition Log__reset: val :=
  rec: "Log__reset" "log" :=
    struct.storeF Log "sz" "log" #0;;
    Log__writeHdr "log";;
    #().

Definition Log__Reset: val :=
  rec: "Log__Reset" "log" :=
    lock.acquire (struct.loadF Log "m" "log");;
    Log__reset "log";;
    lock.release (struct.loadF Log "m" "log");;
    #().
