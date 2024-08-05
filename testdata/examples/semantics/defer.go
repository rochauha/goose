package semantics

func deferSimple() *uint64 {
	x := new(uint64)
	for i := 0; i < 10; i++ {
		defer func() {
			*x += 1
		}()
	}
	return x
}

func testDefer() bool {
	return *(deferSimple()) == 10
}
