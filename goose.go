// Package goose implements conversion from Go source to Perennial definitions.
//
// The exposed interface allows converting individual files as well as whole
// packages to a single Coq Ast with all the converted definitions, which
// include user-defined structs in Go as Coq records and a Perennial procedure
// for each Go function.
//
// See the Goose README at https://github.com/goose-lang/goose for a high-level
// overview. The source also has some design documentation at
// https://github.com/goose-lang/goose/tree/master/docs.
package goose

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goose-lang/goose/glang"
	"golang.org/x/tools/go/packages"
)

// Ctx is a context for resolving Go code's types and source code
type Ctx struct {
	info    *types.Info
	pkgPath string
	errorReporter

	// XXX: this is so we can determine the expected return type when handling a
	// `returnStmt` so the appropriate conversion is inserted
	curFuncType *types.Signature

	// Should be set to true when encountering a defer statement in the body of
	// a function to communicate to its top-level funcDecl that it should
	// include a defer prelude+prologue.
	usesDefer *bool

	dep *depTracker
}

func getFfi(pkg *packages.Package) string {
	seenFfis := make(map[string]struct{})
	packages.Visit([]*packages.Package{pkg},
		func(pkg *packages.Package) bool {
			// the dependencies of an FFI are not considered as being used; this
			// allows one FFI to be built on top of another
			if _, ok := ffiMapping[pkg.PkgPath]; ok {
				return false
			}
			return true
		},
		func(pkg *packages.Package) {
			if ffi, ok := ffiMapping[pkg.PkgPath]; ok {
				seenFfis[ffi] = struct{}{}
			}
		},
	)

	if len(seenFfis) > 1 {
		panic(fmt.Sprintf("multiple ffis used %v", seenFfis))
	}
	for ffi := range seenFfis {
		return ffi
	}
	return "none"
}

// NewPkgCtx initializes a context based on a properly loaded package
func NewPkgCtx(pkg *packages.Package) Ctx {
	return Ctx{
		info:          pkg.TypesInfo,
		pkgPath:       pkg.PkgPath,
		errorReporter: newErrorReporter(pkg.Fset),
		dep:           newDepTracker(),
	}
}

func (ctx Ctx) field(f *ast.Field) glang.FieldDecl {
	if len(f.Names) > 1 {
		ctx.futureWork(f, "multiple fields for same type (split them up)")
		return glang.FieldDecl{}
	}
	if len(f.Names) == 0 {
		ctx.unsupported(f, "unnamed field/parameter")
		return glang.FieldDecl{}
	}
	return glang.FieldDecl{
		Name: f.Names[0].Name,
		Type: ctx.glangTypeFromExpr(f.Type),
	}
}

func (ctx Ctx) paramList(fs *ast.FieldList) []glang.FieldDecl {
	var decls []glang.FieldDecl
	for _, f := range fs.List {
		ty := ctx.glangTypeFromExpr(f.Type)
		for _, name := range f.Names {
			decls = append(decls, glang.FieldDecl{
				Name: name.Name,
				Type: ty,
			})
		}
		if len(f.Names) == 0 { // Unnamed parameter
			decls = append(decls, glang.FieldDecl{
				Name: "",
				Type: ty,
			})
		}
	}
	return decls
}

func (ctx Ctx) typeParamList(fs *ast.FieldList) []glang.TypeIdent {
	var typeParams []glang.TypeIdent
	if fs == nil {
		return nil
	}
	for _, f := range fs.List {
		for _, name := range f.Names {
			typeParams = append(typeParams, glang.TypeIdent(name.Name))
		}
		if len(f.Names) == 0 { // Unnamed parameter
			ctx.unsupported(fs, "unnamed type parameters")
		}
	}
	return typeParams
}

func addSourceDoc(doc *ast.CommentGroup, comment *string) {
	if doc == nil {
		return
	}
	if *comment != "" {
		*comment += "\n\n"
	}
	*comment += strings.TrimSuffix(doc.Text(), "\n")
}

func (ctx Ctx) addSourceFile(d *ast.FuncDecl, comment *string) {
	if *comment != "" {
		*comment += "\n\n"
	}
	f := ctx.fset.Position(d.Name.Pos())
	f.Filename = filepath.Base(f.Filename)
	*comment += "go: " + f.String()
}

func (ctx Ctx) methodSet(t *types.Named) []glang.Decl {
	typeName := t.Obj().Name()

	// Don't try to generate msets for interfaces, since Goosed code will never
	// have to call `interface.make` on it.
	if _, ok := t.Underlying().(*types.Interface); ok {
		return nil
	}

	directMethods := make(map[string]bool)
	// construct method set for T
	mset := glang.MethodSetDecl{DeclName: fmt.Sprintf("%s__mset", typeName)}
	msetPtr := glang.MethodSetDecl{DeclName: fmt.Sprintf("%s__mset_ptr", typeName)}
	func() {
		_, defName := mset.DefName()
		ctx.dep.setCurrentName(defName)
		defer ctx.dep.unsetCurrentName()

		goMset := types.NewMethodSet(t)
		for i := range goMset.Len() {
			selection := goMset.At(i)

			var expr glang.Expr
			if len(selection.Index()) > 1 {
				expr = glang.IdentExpr("$recv")
				expr = ctx.selectionMethod(false, expr, selection, t.Obj())
				expr = glang.FuncLit{Args: []glang.FieldDecl{{Name: "$recv"}}, Body: expr}
			} else {
				n := glang.TypeMethod(typeName, t.Method(selection.Index()[0]).Name())
				expr = glang.GallinaIdent(n)
				ctx.dep.addDep(n)
			}

			name := selection.Obj().Name()
			directMethods[name] = true
			mset.MethodNames = append(mset.MethodNames, name)
			mset.Methods = append(mset.Methods, expr)
		}
	}()

	// construct method set for *T
	_, defName := msetPtr.DefName()
	ctx.dep.setCurrentName(defName)
	defer ctx.dep.unsetCurrentName()

	goMsetPtr := types.NewMethodSet(types.NewPointer(t))
	for i := range goMsetPtr.Len() {
		selection := goMsetPtr.At(i)

		var expr glang.Expr

		if len(selection.Index()) == 1 && !directMethods[selection.Obj().Name()] {
			n := glang.TypeMethod(typeName, t.Method(selection.Index()[0]).Name())
			expr = glang.GallinaIdent(n)
			ctx.dep.addDep(n)
		} else {
			expr = glang.IdentExpr("$recvAddr")
			expr = ctx.selectionMethod(false, expr, selection, t.Obj())
			expr = glang.FuncLit{Args: []glang.FieldDecl{{Name: "$recvAddr"}}, Body: expr}
		}

		name := goMsetPtr.At(i).Obj().Name()
		msetPtr.MethodNames = append(msetPtr.MethodNames, name)
		msetPtr.Methods = append(msetPtr.Methods, expr)
	}

	return []glang.Decl{mset, msetPtr}
}

func (ctx Ctx) typeDecl(spec *ast.TypeSpec) []glang.Decl {
	if spec.TypeParams != nil {
		ctx.futureWork(spec, "generic named type (e.g. no generic structs)")
	}
	var r []glang.Decl

	func() {
		ctx.dep.setCurrentName(spec.Name.Name)
		defer ctx.dep.unsetCurrentName()
		r = append(r, glang.TypeDecl{
			Name: spec.Name.Name,
			Body: ctx.glangTypeFromExpr(spec.Type),
		})
	}()

	if t, ok := ctx.typeOf(spec.Name).(*types.Named); ok {
		r = append(r, ctx.methodSet(t)...)
	}

	return r
}

// TODO: make this the input to handleImplicitConversion?
type exprWithInfo struct {
	e glang.Expr
	t types.Type // for conversions
	n ast.Node   // for printing a location in error messages
}

func (ctx Ctx) sliceLiteralAux(es []exprWithInfo, expectedType types.Type) glang.Expr {
	var expr glang.Expr = glang.GallinaIdent("slice.nil")

	if len(es) > 0 {
		var sliceLitArgs []glang.Expr
		for i := 0; i < len(es); i++ {
			sliceLitArgs = append(sliceLitArgs, glang.IdentExpr(fmt.Sprintf("$sl%d", i)))
		}
		expr = glang.NewCallExpr(glang.GallinaIdent("slice.literal"),
			ctx.glangType(es[0].n, expectedType),
			glang.ListExpr(sliceLitArgs))

		for i := len(es); i > 0; i-- {
			expr = glang.LetExpr{
				Names:   []string{fmt.Sprintf("$sl%d", i-1)},
				ValExpr: ctx.handleImplicitConversion(es[i-1].n, es[i-1].t, expectedType, es[i-1].e),
				Cont:    expr,
			}
		}
		expr = glang.ParenExpr{Inner: expr}
	}
	return expr
}

func (ctx Ctx) sliceLiteral(es []ast.Expr, expectedType types.Type) glang.Expr {
	var exprs []exprWithInfo
	for i := range len(es) {
		exprs = append(exprs, exprWithInfo{e: ctx.expr(es[i]), t: ctx.typeOf(es[i]), n: es[i]})
	}
	return ctx.sliceLiteralAux(exprs, expectedType)
}

func (ctx Ctx) arrayLiteral(e *ast.CompositeLit, expectedType types.Type) glang.Expr {
	var arrayLitArgs []glang.Expr
	for i := 0; i < len(e.Elts); i++ {
		arrayLitArgs = append(arrayLitArgs, glang.IdentExpr(fmt.Sprintf("$ar%d", i)))
	}
	var expr glang.Expr = glang.NewCallExpr(glang.GallinaIdent("array.literal"),
		glang.ListExpr(arrayLitArgs))

	for i := len(e.Elts); i > 0; i-- {
		elt := e.Elts[i-1]
		expr = glang.LetExpr{
			Names:   []string{fmt.Sprintf("$ar%d", i-1)},
			ValExpr: ctx.handleImplicitConversion(elt, ctx.typeOf(elt), expectedType, ctx.expr(elt)),
			Cont:    expr,
		}
	}
	expr = glang.ParenExpr{Inner: expr}
	return expr
}

// Deals with the arguments, but does not actually invoke the function. That
// should be done in the continuation. The continuation can assume the arguments
// are bound to "a0", "$a1", ....
func (ctx Ctx) callExprPrelude(call *ast.CallExpr, cont glang.Expr) (expr glang.Expr) {
	if f, ok := call.Fun.(*ast.Ident); ok {
		if ctx.info.Instances[f].TypeArgs.Len() > 0 {
			ctx.unsupported(f, "generic function")
		}
	}

	funcSig, ok := ctx.typeOf(call.Fun).Underlying().(*types.Signature)
	if !ok {
		ctx.nope(call.Fun, "function should have signature type, got %T", types.Unalias(ctx.typeOf(call.Fun)))
	}

	expr = cont

	var intermediates []exprWithInfo
	intermediatesDone := false
	// look for special case of multi-return pass through
	if len(call.Args) == 1 {
		if tupleType, ok := ctx.typeOf(call.Args[0]).(*types.Tuple); ok {
			intermediatesDone = true
			for i := range tupleType.Len() {
				intermediates = append(intermediates,
					exprWithInfo{
						e: glang.IdentExpr(fmt.Sprintf("$ret%d", i)),
						t: tupleType.At(i).Type(),
						n: call.Args[0],
					})
			}
			// destructure the inner call at the end
			defer func() {
				var names []string
				for i := range tupleType.Len() {
					names = append(names, fmt.Sprintf("$ret%d", i))
				}
				expr = glang.LetExpr{
					Names:   names,
					ValExpr: glang.ParenExpr{Inner: ctx.expr(call.Args[0])},
					Cont:    expr,
				}
			}()
		}
	}
	if !intermediatesDone {
		for i := range len(call.Args) {
			intermediates = append(intermediates,
				exprWithInfo{
					e: ctx.expr(call.Args[i]),
					t: ctx.typeOf(call.Args[i]),
					n: call.Args[i],
				})
		}
	}

	var i int = funcSig.Params().Len()
	if funcSig.Variadic() && call.Ellipsis == token.NoPos {
		// construct a slice literal for all the arguments including and after
		// the last one listed in funcSig.
		expr = glang.LetExpr{
			Names: []string{fmt.Sprintf("$a%d", i-1)},
			ValExpr: ctx.sliceLiteralAux(intermediates[i-1:],
				funcSig.Params().At(i-1).Type().(*types.Slice).Elem()),
			Cont: expr,
		}
		i--
	}
	for ; i > 0; i-- {
		expr = glang.LetExpr{
			Names: []string{fmt.Sprintf("$a%d", i-1)},
			ValExpr: ctx.handleImplicitConversion(intermediates[i-1].n,
				intermediates[i-1].t, funcSig.Params().At(i-1).Type(), intermediates[i-1].e),
			Cont: expr,
		}
	}
	return
}

// integerConversion generates an expression for converting x to an integer
// of a specific width
//
// s is only used for error reporting
func (ctx Ctx) integerConversion(s ast.Node, x ast.Expr, width int) glang.Expr {
	if info, ok := getIntegerType(ctx.typeOf(x)); ok {
		if info.isUntyped {
			ctx.todo(s, "integer conversion from untyped int to uint64")
		}
		if info.width == width {
			return ctx.expr(x)
		}
		return glang.NewCallExpr(glang.GallinaIdent(fmt.Sprintf("to_u%d", width)),
			ctx.expr(x))
	}
	ctx.unsupported(s, "casts from unsupported type %v to uint%d",
		ctx.typeOf(x), width)
	return nil
}

func (ctx Ctx) conversionExpr(s *ast.CallExpr) glang.Expr {
	if len(s.Args) != 1 {
		ctx.nope(s, "expect exactly one argument in a conversion")
	}
	toType := ctx.info.TypeOf(s.Fun).Underlying()
	fromType := ctx.info.TypeOf(s.Args[0]).Underlying()
	if types.Identical(toType, fromType) {
		return ctx.expr(s.Args[0])
	}
	switch toType := toType.(type) {
	case *types.Basic:
		// handle conversions between integer types
		if fromType, ok := fromType.(*types.Basic); ok {
			if (fromType.Info()&types.IsInteger) != 0 &&
				toType.Info()&types.IsInteger == 0 {
				ctx.unsupported(s, "converting from integer type to non-integer type")
			}
			switch toType.Kind() {
			case types.Uint64:
				return ctx.integerConversion(s, s.Args[0], 64)
			case types.Uint32:
				return ctx.integerConversion(s, s.Args[0], 32)
			case types.Uint8:
				return ctx.integerConversion(s, s.Args[0], 8)
			}
		}

		// handle `string(b)`, where b : []byte
		if toType.Kind() == types.String && isByteSlice(fromType) {
			return glang.NewCallExpr(glang.GallinaIdent("string.from_bytes"), ctx.expr(s.Args[0]))
		}
	case *types.Slice:
		// handle `[]byte(s)`, where s : string
		if eltType, ok := toType.Elem().Underlying().(*types.Basic); ok &&
			eltType.Kind() == types.Byte && isString(fromType) {
			return glang.NewCallExpr(glang.GallinaIdent("string.to_bytes"), ctx.expr(s.Args[0]))
		}
	}

	return ctx.handleImplicitConversion(s, fromType, toType, ctx.expr(s.Args[0]))
	// ctx.unsupported(s, "explicit conversion from %s to %s", fromType, toType)
	// return nil
}

// This handles:
// - make() and new() because they uniquely take a type as a normal parameter.
// - array len() and cap() because they are untyped functions
func (ctx Ctx) maybeHandleSpecialBuiltin(s *ast.CallExpr) (glang.Expr, bool) {
	if !ctx.info.Types[s.Fun].IsBuiltin() {
		return nil, false
	}

	switch s.Fun.(*ast.Ident).Name {
	case "make":
		sig := ctx.typeOf(s.Fun).(*types.Signature)
		switch ty := sig.Params().At(0).Type().Underlying().(type) {
		case *types.Slice:
			elt := ctx.glangType(s.Fun, ty.Elem())
			switch sig.Params().Len() {
			case 2:
				return glang.NewCallExpr(glang.GallinaIdent("slice.make2"), elt,
					ctx.expr(s.Args[1])), true
			case 3:
				return glang.NewCallExpr(glang.GallinaIdent("slice.make3"), elt,
					ctx.expr(s.Args[1]), ctx.expr(s.Args[2])), true
			default:
				ctx.nope(s, "Too many or too few arguments in slice construction")
				return glang.CallExpr{}, true
			}
		case *types.Map:
			return glang.NewCallExpr(glang.GallinaIdent("map.make"),
				ctx.glangType(s.Args[0], ty.Key()),
				ctx.glangType(s.Args[0], ty.Elem()),
				glang.UnitLiteral{}), true
		case *types.Chan:
			return glang.NewCallExpr(glang.GallinaIdent("chan.make"),
				ctx.glangType(s.Args[0], ty.Elem()),
				glang.UnitLiteral{}), true
		default:
			ctx.unsupported(s, "make should be slice or map, got %v", ty)
		}
	case "new":
		sig := ctx.typeOf(s.Fun).(*types.Signature)
		ty := ctx.glangType(s.Args[0], sig.Params().At(0).Type())
		return glang.RefExpr{
			X:  glang.NewCallExpr(glang.GallinaIdent("zero_val"), ty),
			Ty: ty,
		}, true
	case "len", "cap":
		if _, ok := ctx.typeOf(s.Fun).(*types.Signature); ok {
			return nil, false
		}
		name := s.Fun.(*ast.Ident).Name
		return glang.NewCallExpr(glang.GallinaIdent(fmt.Sprintf("array.%s", name)),
			ctx.glangType(s, ctx.typeOf(s.Args[0]))), true
	}

	return nil, false
}

func (ctx Ctx) getNumParams(e ast.Expr) int {
	funcSig, ok := ctx.typeOf(e).Underlying().(*types.Signature)
	if !ok {
		ctx.nope(e, "function should have signature type, got %T", types.Unalias(ctx.typeOf(e)))
	}
	return funcSig.Params().Len()
}

func (ctx Ctx) callExpr(s *ast.CallExpr) glang.Expr {
	if ctx.info.Types[s.Fun].IsType() {
		return ctx.conversionExpr(s)
	} else if e, ok := ctx.maybeHandleSpecialBuiltin(s); ok {
		return e
	} else {
		var args []glang.Expr
		for i := range ctx.getNumParams(s.Fun) {
			args = append(args, glang.IdentExpr(fmt.Sprintf("$a%d", i)))
		}
		return ctx.callExprPrelude(s, glang.NewCallExpr(ctx.expr(s.Fun), args...))
	}
}

func (ctx Ctx) qualifiedName(obj types.Object) string {
	name := obj.Name()
	if obj.Pkg() == nil {
		return name
	} else if ctx.pkgPath == obj.Pkg().Path() {
		// no module name needed
		return name
	}
	return fmt.Sprintf("%s.%s", obj.Pkg().Name(), name)
}

func (ctx Ctx) selectorExprAddr(e *ast.SelectorExpr) glang.Expr {
	selection := ctx.info.Selections[e]
	if selection == nil {
		pkg, ok := getIdent(e.X)
		if !ok {
			ctx.unsupported(e, "expected package selector with idtent, got %T", e.X)
		}
		return glang.PackageIdent{
			Package: pkg,
			Ident:   e.Sel.Name,
		}
	}

	switch selection.Kind() {
	case types.FieldVal:
		if ctx.info.Types[e.X].Addressable() {
			expr, curType, index := ctx.exprAddr(e.X), selection.Recv(), selection.Index()
			ctx.fieldAddrSelection(e, index, &curType, &expr)
			return expr
		} else {
			expr, curType, index := ctx.expr(e.X), selection.Recv(), selection.Index()
			ctx.fieldSelection(e.X, &index, &curType, &expr)
			if len(index) == 0 {
				ctx.nope(e, "expected addressable expression")
			}
			ctx.fieldAddrSelection(e.X, index, &curType, &expr)
			return expr
		}
	case types.MethodVal:
		ctx.nope(e, "method val is not addressable")
	case types.MethodExpr:
		ctx.nope(e, "method expr is not addressable")
	}
	ctx.nope(e, "unexpected kinf of selection")
	panic("unreachable")
}

// Requires that `old(expr) : old(curType)` and that `old(curType)` be a struct
// type. This walks down the selection specified by `index` up until it sees a
// pointer field, then returns. Its intent is to be combined with
// fieldAddrSelection to go the rest of the way.
//
// If len(index) > 0, then `expr : ptr<curType>`.
// If len(index) == 0, then `expr : curType`.
func (ctx Ctx) fieldSelection(n locatable, index *[]int, curType *types.Type, expr *glang.Expr) {
	for ; len(*index) > 0; *index = (*index)[1:] {
		i := (*index)[0]
		info, ok := ctx.getStructInfo(*curType)
		if !ok {
			ctx.nope(n, "expected (pointer to) struct type for base of selector, got %s", *curType)
		}

		if info.throughPointer {
			// XXX: this is to feed into fieldAddrSelection (see comment above this func).
			*curType = (*curType).(*types.Pointer).Elem()
			return
		}
		v := info.structType.Field(i)
		*expr = glang.NewCallExpr(glang.GallinaIdent("struct.field_get"),
			glang.GallinaIdent(info.name), glang.GallinaString(v.Name()), *expr)
		ctx.dep.addDep(info.name)
		*curType = v.Type()
	}
	return
}

// Requires `old(expr) : ptr<old(curType)>`.
// This walks down the selection specified by `index` all the way to the end,
// returning an expression representing the address of that selected field.
func (ctx Ctx) fieldAddrSelection(n locatable, index []int, curType *types.Type, expr *glang.Expr) {
	for _, i := range index {
		info, ok := ctx.getStructInfo(*curType)
		ctx.dep.addDep(info.name)
		if !ok {
			if _, ok := (*curType).(*types.Struct); ok {
				ctx.unsupported(n, "anonymous struct")
			}
			ctx.nope(n, "expected (pointer to) struct type for base of selector, got %s", *curType)
		}
		if info.throughPointer {
			*expr = glang.DerefExpr{X: *expr, Ty: ctx.glangType(n, *curType)}
		}
		v := info.structType.Field(i)

		*expr = glang.NewCallExpr(glang.GallinaIdent("struct.field_ref"),
			glang.GallinaIdent(info.name), glang.GallinaString(v.Name()), *expr)
		*curType = v.Type()
	}
	return
}

// requires `!addressable -> (expr : selection.Recv())`
// requires `addressable -> (expr : ptr<selection.Recv()>)`
func (ctx Ctx) selectionMethod(addressable bool, expr glang.Expr,
	selection *types.Selection, l locatable) glang.Expr {

	index, curType := selection.Index(), selection.Recv()
	fnIndex, index := index[len(index)-1], index[:len(index)-1]

	if !addressable {
		ctx.fieldSelection(l, &index, &curType, &expr)
	}
	if addressable || len(index) > 0 {
		ctx.fieldAddrSelection(l, index, &curType, &expr)
		// expr : ptrT<curType>
		if _, ok := types.Unalias(curType).(*types.Pointer); ok {
			expr = glang.DerefExpr{X: expr, Ty: ctx.glangType(l, curType)}
		} else {
			curType = types.NewPointer(curType)
		}
	}
	// now, (expr : curType), and there's no deref unless it's unavoidale.

	// At this point, (expr : curType), and (curType = ptr<named>) if the
	// original expression is an address or is addressable, and (curType =
	// named) otherwise.
	if info, ok := ctx.getInterfaceInfo(curType); ok {
		if info.throughPointer {
			expr = glang.DerefExpr{X: expr, Ty: ctx.glangType(l, curType.(*types.Pointer).Elem())}
		}
		return glang.NewCallExpr(glang.GallinaIdent("interface.get"),
			glang.GallinaString(info.interfaceType.Method(fnIndex).Name()), expr,
		)
	}

	if pointerT, ok := types.Unalias(curType).(*types.Pointer); ok {
		t, ok := types.Unalias(pointerT.Elem()).(*types.Named)
		if !ok {
			ctx.nope(l, "methods can only be called on a pointer if the base type is a defined type, not %s", pointerT.Elem())
		}
		m := glang.TypeMethod(ctx.qualifiedName(t.Obj()), t.Method(fnIndex).Name())

		// check for recursive call
		var f glang.Expr
		if sc := t.Method(fnIndex).Scope(); sc != nil && sc.Contains(l.Pos()) {
			f = glang.IdentExpr(m)
		} else {
			f = glang.GallinaIdent(m)
			ctx.dep.addDep(m)
		}

		// XXX: The following line does not give the same result as
		// `funcSig2, ok := selection.Type().(*types.Signature)`
		// The latter seems has a different receiver type. It seems to rely
		// on the method set of a struct already including the methods of its embedded fields.
		funcSig, ok := t.Method(fnIndex).Type().(*types.Signature)
		if !ok {
			ctx.nope(l, "func should have signature type, got %s", t.Method(fnIndex).Type())
		}

		if _, ok := types.Unalias(funcSig.Recv().Type()).(*types.Pointer); ok {
			return glang.NewCallExpr(f, expr)
		} else {
			// ctx.unsupported(e, "%v", funcSig)
			return glang.NewCallExpr(f, glang.DerefExpr{X: expr, Ty: ctx.glangType(l, t)})
		}
	} else if t, ok := types.Unalias(curType).(*types.Named); ok {
		var typeName = ctx.qualifiedName(t.Obj())
		m := glang.TypeMethod(typeName, t.Method(fnIndex).Name())

		var f glang.Expr
		if sc := t.Method(fnIndex).Scope(); sc != nil && sc.Contains(l.Pos()) {
			f = glang.IdentExpr(m)
		} else {
			f = glang.GallinaIdent(m)
			ctx.dep.addDep(m)
		}

		funcSig, ok := t.Method(fnIndex).Type().(*types.Signature)
		if !ok {
			ctx.nope(l, "func should have signature type, got %s", t.Method(fnIndex).Type())
		}

		if _, ok := types.Unalias(funcSig.Recv().Type()).(*types.Pointer); ok {
			ctx.nope(l, "receiver must be pointer, but selectorExpr has non-addressable base")
		} else {
			return glang.NewCallExpr(f, expr)
		}
	}
	ctx.nope(l, "methods can only be called on (pointers to) defined types, not %s", curType)
	return nil
}

func (ctx Ctx) selectorExpr(e *ast.SelectorExpr) glang.Expr {
	selection := ctx.info.Selections[e]
	if selection == nil {
		pkg, ok := getIdent(e.X)
		if !ok {
			ctx.unsupported(e, "expected package selector with ident, got %T", e.X)
		}
		return glang.PackageIdent{
			Package: pkg,
			Ident:   e.Sel.Name,
		}
	}

	switch selection.Kind() {
	case types.MethodExpr:
		ctx.unsupported(e, "method expr")
	case types.FieldVal:
		var expr glang.Expr
		index, curType := selection.Index(), selection.Recv()

		if ctx.info.Types[e.X].Addressable() {
			expr = ctx.exprAddr(e.X)
		} else {
			expr = ctx.expr(e.X)
			ctx.fieldSelection(e.X, &index, &curType, &expr)
		}
		if len(index) > 0 {
			ctx.fieldAddrSelection(e.X, index, &curType, &expr)
			expr = glang.DerefExpr{X: expr, Ty: ctx.glangType(e.Sel, curType)}
		}
		return expr

	case types.MethodVal:
		// 2*2 cases: receiver type could be (T) or (*T), and e.X type
		// (including embedded fields) could be (T) or (*T).

		if ctx.info.Types[e.X].Addressable() {
			return ctx.selectionMethod(true, ctx.exprAddr(e.X), selection, e)
		} else {
			return ctx.selectionMethod(false, ctx.expr(e.X), selection, e)
		}
	}

	panic("unreachable")
}

func (ctx Ctx) compositeLiteral(e *ast.CompositeLit) glang.Expr {
	if t, ok := ctx.typeOf(e).Underlying().(*types.Slice); ok {
		return ctx.sliceLiteral(e.Elts, t.Elem())
	} else if t, ok := ctx.typeOf(e).Underlying().(*types.Array); ok {
		return ctx.arrayLiteral(e, t.Elem())
	}
	info, ok := ctx.getStructInfo(ctx.typeOf(e))
	if ok {
		return ctx.structLiteral(info, e)
	}
	ctx.unsupported(e, "composite literal of type %v", ctx.typeOf(e))
	return nil
}

func (ctx Ctx) structLiteral(info structTypeInfo, e *ast.CompositeLit) glang.StructLiteral {
	ctx.dep.addDep(info.name)
	lit := glang.NewStructLiteral(info.name)
	isUnkeyedStruct := false

	getFieldType := func(fieldName string) types.Type {
		for i := range info.structType.NumFields() {
			if info.structType.Field(i).Name() == fieldName {
				return info.structType.Field(i).Type()
			}
		}
		ctx.nope(e, "field is not a part of the struct")
		return types.NewTuple()
	}

	for _, el := range e.Elts {
		switch el := el.(type) {
		case *ast.KeyValueExpr:
			ident, ok := getIdent(el.Key)
			if !ok {
				ctx.noExample(el.Key, "struct field keyed by non-identifier %+v", el.Key)
				return glang.StructLiteral{}
			}
			lit.AddField(ident,
				ctx.handleImplicitConversion(el.Value,
					ctx.typeOf(el.Value),
					getFieldType(ident),
					ctx.expr(el.Value)))
		default:
			isUnkeyedStruct = true
		}
	}
	if isUnkeyedStruct {
		if len(e.Elts) != info.structType.NumFields() {
			ctx.nope(e, "expected as many elements are there are struct fields in unkeyed literal")
		}
		for i := range info.structType.NumFields() {
			lit.AddField(info.structType.Field(i).Name(),
				ctx.handleImplicitConversion(e.Elts[i],
					ctx.typeOf(e.Elts[i]),
					info.structType.Field(i).Type(),
					ctx.expr(e.Elts[i]),
				))
		}
	}
	return lit
}

func (ctx Ctx) basicLiteral(e *ast.BasicLit) glang.Expr {
	tv := ctx.info.Types[e]
	if tv.Value == nil {
		ctx.nope(e, "basic literal should have a const val")
	}
	t, v := ctx.constantLiteral(e, tv.Value)
	return ctx.handleImplicitConversion(e, t, tv.Type, v)
}

func (ctx Ctx) isNilCompareExpr(e *ast.BinaryExpr) bool {
	if !(e.Op == token.EQL || e.Op == token.NEQ) {
		return false
	}
	return ctx.info.Types[e.Y].IsNil()
}

func (ctx Ctx) typeJoin(n ast.Node, t1, t2 types.Type) types.Type {
	if types.AssignableTo(t1, t2) {
		return t2
	} else if types.AssignableTo(t2, t1) {
		return t1
	} else {
		ctx.nope(n, "comparison between non-assignable types")
		return nil
	}
}

func (ctx Ctx) binExpr(e *ast.BinaryExpr) (expr glang.Expr) {
	// XXX: according to the Go spec, comparisons can occur between types that
	// are "assignable" to one another. This may require a conversion, so we
	// here convert to the appropriate type here.
	//
	// XXX: this also handles converting untyped constants to a typed constant
	xT, yT := ctx.typeOf(e.X), ctx.typeOf(e.Y)
	compType := ctx.typeJoin(e, xT, yT).Underlying()
	var op glang.BinOp = -1
	if t, ok := compType.(*types.Basic); ok {
		switch t.Kind() {
		case types.UntypedInt:
			op, ok = untypedIntOps[e.Op]
			if !ok {
				// use const
				tv := ctx.info.Types[e]
				if tv.Value == nil {
					ctx.nope(e, "untyped integer expression without constant value")
				}
				t, v := ctx.constantLiteral(e, tv.Value)
				return ctx.handleImplicitConversion(e, t, tv.Type, v)
			}
			switch op {
			case glang.OpEqualsZ, glang.OpLessThanZ, glang.OpLessEqZ, glang.OpGreaterThanZ, glang.OpGreaterEqZ:
				defer func() {
					expr = glang.BoolVal{Value: expr}
				}()
			case glang.OpNotEquals:
				op = glang.OpEqualsZ
				defer func() {
					expr = glang.BoolVal{Value: glang.GallinaNotExpr{X: expr}}
				}()
			}
		case types.Uint, types.Uint64, types.Uint32, types.Uint16, types.Uint8:
			op, ok = unsignedIntOps[e.Op]
			if !ok {
				ctx.unsupported(e, "unsupported binary operation on unsinged integers")
			}
		case types.Int, types.Int64, types.Int32, types.Int16, types.Int8:
			op, ok = signedIntOps[e.Op]
			if !ok {
				ctx.unsupported(e, "unsupported binary operation on singed integers")
			}
		case types.UntypedBool, types.Bool:
			op, ok = boolOps[e.Op]
			if !ok {
				ctx.unsupported(e, "unsupported binary operation on booleans")
			}
		case types.String, types.UntypedString:
			op, ok = stringOps[e.Op]
			if !ok {
				ctx.unsupported(e, "unsupported binary operation on strings")
			}
		}
		if (t.Info() & types.IsInteger) != 0 {
			switch op {
			case glang.OpMinus, glang.OpMul, glang.OpRem, glang.OpPlus, glang.OpQuot:
				defer func() {
					expr = ctx.handleImplicitConversion(e, compType, ctx.typeOf(e), expr)
				}()
			}
		}
	} else {
		op, ok = generalOps[e.Op]
		if !ok {
			ctx.unsupported(e, "binary operation on general type")
		}
	}
	if op == -1 {
		ctx.unsupported(e, "unknown op")
	}

	expr = glang.BinaryExpr{
		X:  ctx.handleImplicitConversion(e.X, xT, compType, ctx.expr(e.X)),
		Op: op,
		Y:  ctx.handleImplicitConversion(e.Y, yT, compType, ctx.expr(e.Y)),
	}

	return expr
}

func (ctx Ctx) sliceExpr(e *ast.SliceExpr) glang.Expr {
	if e.Slice3 {
		ctx.unsupported(e, "3-index slice")
		return nil
	}
	if e.Max != nil {
		ctx.unsupported(e, "setting the max capacity in a slice expression is not supported")
		return nil
	}
	if e.Low == nil && e.High == nil {
		ctx.unsupported(e, "complete slice doesn't do anything")
	}

	x := ctx.expr(e.X)
	var lowExpr glang.Expr = glang.Int64Val{Value: glang.ZLiteral{Value: big.NewInt(0)}}
	var highExpr glang.Expr = glang.NewCallExpr(glang.GallinaIdent("slice.len"), glang.IdentExpr("$s"))
	if e.Low != nil {
		lowExpr = ctx.expr(e.Low)
	}
	if e.High != nil {
		highExpr = ctx.expr(e.High)
	}
	if _, ok := ctx.typeOf(e.X).Underlying().(*types.Slice); !ok {
		ctx.unsupported(e, "taking a slice of an object with type %s", ctx.typeOf(e.X))
	}
	return glang.LetExpr{
		Names:   []string{"$s"},
		ValExpr: x,
		Cont: glang.NewCallExpr(glang.GallinaIdent("slice.slice"),
			ctx.glangType(e, sliceElem(ctx.typeOf(e.X))),
			glang.IdentExpr("$s"), lowExpr, highExpr),
	}
}

func (ctx Ctx) nilExpr(e *ast.Ident) glang.Expr {
	t := ctx.typeOf(e)
	switch t.(type) {
	case *types.Pointer:
		return glang.GallinaIdent("null")
	case *types.Slice:
		return glang.GallinaIdent("slice.nil")
	case *types.Basic:
		// TODO: this gets triggered for all of our unit tests because the
		//  nil identifier is mapped to an untyped nil object.
		//  This seems wrong; the runtime representation of each of these
		//  uses depends on the type, so Go must know how they're being used.
		return glang.GallinaIdent("BUG: this should get overwritten by handleImplicitConversion")
	default:
		ctx.unsupported(e, "nil of type %v (not pointer or slice)", t)
		return nil
	}
}

func (ctx Ctx) unaryExpr(e *ast.UnaryExpr, isSpecial bool) glang.Expr {
	if e.Op == token.NOT {
		return glang.NotExpr{X: ctx.expr(e.X)}
	}
	if e.Op == token.XOR {
		return glang.NotExpr{X: ctx.expr(e.X)}
	}
	if e.Op == token.AND {
		if x, ok := e.X.(*ast.IndexExpr); ok {
			// e is &a[b] where x is a.b
			if xTy, ok := ctx.typeOf(x.X).(*types.Slice); ok {
				return glang.NewCallExpr(glang.GallinaIdent("slice.elem_ref"),
					ctx.glangType(e, xTy.Elem()),
					ctx.expr(x.X), ctx.expr(x.Index))
			}
		}
		if info, ok := ctx.getStructInfo(ctx.typeOf(e.X)); ok {
			structLit, ok := e.X.(*ast.CompositeLit)
			if ok {
				// e is &s{...} (a struct literal)
				sl := ctx.structLiteral(info, structLit)
				return glang.RefExpr{
					X:  sl,
					Ty: ctx.glangType(e.X, ctx.typeOf(e.X)),
				}
			}
		}
		// e is something else
		return ctx.exprAddr(e.X)
	}
	if e.Op == token.ARROW {
		var expr glang.Expr = glang.NewCallExpr(glang.GallinaIdent("chan.receive"), ctx.expr(e.X))
		if !isSpecial {
			expr = glang.NewCallExpr(glang.GallinaIdent("Fst"), expr)
		}
		return expr
	}
	ctx.unsupported(e, "unary expression %s", e.Op)
	return nil
}

func (ctx Ctx) variable(s *ast.Ident) glang.Expr {
	if _, ok := ctx.info.Uses[s].(*types.Const); ok {
		ctx.dep.addDep(s.Name)
		return glang.GallinaIdent(s.Name)
	}
	return glang.DerefExpr{X: glang.IdentExpr(s.Name), Ty: ctx.glangType(s, ctx.typeOf(s))}
}

func (ctx Ctx) function(s *ast.Ident) glang.Expr {
	fun := ctx.info.Uses[s].(*types.Func)
	if sc := fun.Scope(); sc != nil && sc.Contains(s.Pos()) {
		return glang.IdentExpr(s.Name)
	}
	ctx.dep.addDep(s.Name)
	return glang.GallinaIdent(s.Name)
}

func (ctx Ctx) goBuiltin(e *ast.Ident) bool {
	s, ok := ctx.info.Uses[e]
	if !ok {
		return false
	}
	return s.Parent() == types.Universe
}

func (ctx Ctx) builtinIdent(e *ast.Ident) glang.Expr {
	switch e.Name {
	case "nil":
		return ctx.nilExpr(e)
	case "true":
		return glang.True
	case "false":
		return glang.False
	case "append":
		sig := ctx.typeOf(e).(*types.Signature)
		t := sig.Params().At(1).Type()
		return glang.NewCallExpr(glang.GallinaIdent("slice.append"),
			ctx.glangType(e, t),
		)
	case "new":
		sig := ctx.typeOf(e).(*types.Signature)
		ctx.todo(e, "new might be better as its own function")
		t := ctx.glangType(e, sig.Params().At(0).Type())
		return glang.RefExpr{
			X:  glang.NewCallExpr(glang.GallinaIdent("zero_val"), t),
			Ty: t,
		}
	case "len":
		sig := ctx.typeOf(e).(*types.Signature)
		switch ty := sig.Params().At(0).Type().Underlying().(type) {
		case *types.Slice:
			return glang.GallinaIdent("slice.len")
		case *types.Map:
			return glang.GallinaIdent("map.len")
		case *types.Basic:
			if ty.Kind() == types.String {
				return glang.GallinaIdent("StringLength")
			}
		default:
			ctx.unsupported(e, "length of object of type %v", ty)
		}
	case "cap":
		sig := ctx.typeOf(e).(*types.Signature)
		switch ty := sig.Params().At(0).Type().Underlying().(type) {
		case *types.Slice:
			return glang.GallinaIdent("slice.cap")
		default:
			ctx.unsupported(e, "capacity of object of type %v", ty)
		}
	case "copy":
		sig := ctx.typeOf(e).(*types.Signature)
		switch ty := sig.Params().At(0).Type().Underlying().(type) {
		case *types.Slice:
			fromTy := sig.Params().At(1).Type().Underlying()
			if types.Identical(ty, fromTy) {
				return glang.NewCallExpr(
					glang.GallinaIdent("slice.copy"),
					ctx.glangType(e, ty.Elem()),
				)
			}
			ctx.unsupported(e, "copy to %v from %v", ty, fromTy)
		default:
			ctx.nope(e, "copy of object of type %v", ty)
		}
	case "delete":
		sig := ctx.typeOf(e).(*types.Signature)
		if _, ok := sig.Params().At(0).Type().Underlying().(*types.Map); !ok {
			ctx.nope(e, "delete on non-map")
		}
		return glang.GallinaIdent("map.delete")
	case "panic":
		return glang.GallinaIdent("Panic")
	case "min", "max":
		sig := ctx.typeOf(e).(*types.Signature)
		if sig.Params().Len() == 0 {
			ctx.nope(e, "min/max with no params")
		}
		// figure out the desired type by taking a type join of everything.
		// XXX: the signature might always be (T, T, T, T, T).
		var t types.Type = sig.Params().At(0).Type().Underlying()
		for i := 0; i < sig.Params().Len(); i++ {
			t = ctx.typeJoin(e, t, sig.Params().At(i).Type())
		}
		if types.Identical(t, types.Typ[types.Uint64]) {
			return glang.NewCallExpr(glang.GallinaIdent(fmt.Sprintf("%sUint64", e.Name)),
				glang.GallinaIdent(fmt.Sprintf("%d", sig.Params().Len())))
		}
		ctx.unsupported(e, "%s with final type %v", e.Name, t)
	case "close":
		return glang.GallinaIdent("chan.close")
	case "iota":
		o := ctx.info.ObjectOf(e)
		t, v := ctx.constantLiteral(e, o.(*types.Const).Val())
		return ctx.handleImplicitConversion(e, t, ctx.typeOf(e), v)
	default:
		ctx.unsupported(e, "builtin identifier of type %v", ctx.typeOf(e))
	}
	return nil
}

func (ctx Ctx) identExpr(e *ast.Ident, isSpecial bool) glang.Expr {
	// XXX: special case for a manually constructed Ident from select recv clause
	if len(e.Name) > 0 && e.Name[0] == '$' {
		var expr glang.Expr = glang.IdentExpr(e.Name)
		if !isSpecial {
			expr = glang.NewCallExpr(glang.GallinaIdent("Fst"), expr)
		}
		return expr
	}

	if ctx.goBuiltin(e) {
		return ctx.builtinIdent(e)
	}

	// check if e refers to a variable,
	obj := ctx.info.ObjectOf(e)
	if constObj, ok := obj.(*types.Const); ok {
		// is a constant
		ctx.dep.addDep(e.Name)
		return ctx.handleImplicitConversion(e, constObj.Type(), ctx.typeOf(e), glang.GallinaIdent(e.Name))
	}
	if _, ok := obj.(*types.Var); ok {
		// is a variable
		return glang.DerefExpr{X: glang.IdentExpr(e.Name), Ty: ctx.glangType(e, ctx.typeOf(e))}
	}
	if _, ok := obj.(*types.Func); ok {
		// is a function
		return ctx.function(e)
	}
	ctx.unsupported(e, "unrecognized kind of identifier; not local variable or global function")
	panic("")
}

func (ctx Ctx) indexExpr(e *ast.IndexExpr, isSpecial bool) glang.Expr {
	xTy := ctx.typeOf(e.X).Underlying()
	switch xTy.(type) {
	case *types.Map:
		e := glang.NewCallExpr(glang.GallinaIdent("map.get"),
			ctx.expr(e.X),
			ctx.expr(e.Index))
		// FIXME: this is non-local. Should decide whether to do "Fst" based on
		// assign statement or parent expression.
		if !isSpecial {
			e = glang.NewCallExpr(glang.GallinaIdent("Fst"), e)
		}
		return e
	case *types.Slice:
		return glang.DerefExpr{
			X:  ctx.exprAddr(e),
			Ty: ctx.glangType(e, ctx.typeOf(e)),
		}
	case *types.Array:
		if ctx.info.Types[e].Addressable() {
			return glang.DerefExpr{
				X:  ctx.exprAddr(e),
				Ty: ctx.glangType(e, ctx.typeOf(e)),
			}
		} else {
			return glang.NewCallExpr(glang.GallinaIdent("array.elem_get"),
				ctx.glangType(e, ctx.typeOf(e)),
				ctx.expr(e.X), ctx.expr(e.Index))
		}
	case *types.Signature:
		ctx.unsupported(e, "generic function %v", xTy)
	}
	ctx.unsupported(e, "index into unknown type %v", xTy)
	return glang.CallExpr{}
}

func (ctx Ctx) derefExpr(e ast.Expr) glang.Expr {
	return glang.DerefExpr{
		X:  ctx.expr(e),
		Ty: ctx.glangType(e, ptrElem(ctx.typeOf(e))),
	}
}

func (ctx Ctx) expr(e ast.Expr) glang.Expr {
	return ctx.exprSpecial(e, false)
}

func (ctx Ctx) funcLit(e *ast.FuncLit) glang.FuncLit {
	fl := glang.FuncLit{}

	ctx.usesDefer = new(bool)

	// ctx is by value, so no need to unset this
	ctx.curFuncType = ctx.typeOf(e.Type).(*types.Signature)
	fl.Args = ctx.paramList(e.Type.Params)
	fl.Body = ctx.blockStmt(e.Body, nil)

	for _, arg := range fl.Args {
		fl.Body = glang.LetExpr{
			Names:   []string{arg.Name},
			ValExpr: glang.RefExpr{Ty: arg.Type, X: glang.IdentExpr(arg.Name)},
			Cont:    fl.Body,
		}
	}

	if *ctx.usesDefer {
		fl.Body = glang.NewCallExpr(glang.GallinaIdent("with_defer:"), fl.Body)
	} else {
		fl.Body = glang.NewCallExpr(glang.GallinaIdent("exception_do"), fl.Body)
	}

	return fl
}

func (ctx Ctx) exprSpecial(e ast.Expr, isSpecial bool) glang.Expr {
	switch e := e.(type) {
	case *ast.CallExpr:
		return ctx.callExpr(e)
	case *ast.Ident:
		return ctx.identExpr(e, isSpecial)
	case *ast.SelectorExpr:
		return ctx.selectorExpr(e)
	case *ast.CompositeLit:
		return ctx.compositeLiteral(e)
	case *ast.BasicLit:
		// ctx.nope(e, "all basic literal should have a const value")
		return ctx.basicLiteral(e)
	case *ast.BinaryExpr:
		return ctx.binExpr(e)
	case *ast.SliceExpr:
		return ctx.sliceExpr(e)
	case *ast.IndexExpr:
		return ctx.indexExpr(e, isSpecial)
	case *ast.UnaryExpr:
		return ctx.unaryExpr(e, isSpecial)
	case *ast.ParenExpr:
		return ctx.expr(e.X)
	case *ast.StarExpr:
		return ctx.derefExpr(e.X)
	case *ast.TypeAssertExpr:
		// TODO: do something with the type
		return ctx.expr(e.X)
	case *ast.FuncLit:
		return ctx.funcLit(e)
	default:
		ctx.unsupported(e, "unexpected expr")
	}
	return nil
}

func (ctx Ctx) stmtList(ss []ast.Stmt, cont glang.Expr) glang.Expr {
	if len(ss) == 0 {
		return glang.DoExpr{Expr: glang.Tt}
	}
	var e glang.Expr = nil
	for len(ss) > 0 {
		stmt := ss[len(ss)-1]
		ss = ss[:len(ss)-1]
		e = ctx.stmt(stmt, e)
	}
	return glang.SeqExpr{Expr: e, Cont: cont}
}

func (ctx Ctx) blockStmt(s *ast.BlockStmt, cont glang.Expr) glang.Expr {
	return ctx.stmtList(s.List, cont)
}

func (ctx Ctx) switchStmt(s *ast.SwitchStmt, cont glang.Expr) (e glang.Expr) {
	var tagExpr glang.Expr = glang.True

	var tagType types.Type = types.Typ[types.Bool]

	if s.Tag != nil {
		tagType = ctx.typeOf(s.Tag)
		tagExpr = ctx.expr(s.Tag)
	}

	// Get default handler
	e = glang.Tt
	for i := len(s.Body.List) - 1; i >= 0; i-- {
		c := s.Body.List[i].(*ast.CaseClause)
		if c.List == nil {
			e = ctx.stmtList(c.Body, nil)
		}
	}

	for i := len(s.Body.List) - 1; i >= 0; i-- {
		c := s.Body.List[i].(*ast.CaseClause)
		if c.List == nil {
			continue
		}

		t := ctx.typeOf(c.List[0])
		eqType := ctx.typeJoin(c.List[0], t, tagType)
		cond := glang.BinaryExpr{
			X:  ctx.handleImplicitConversion(c.List[0], tagType, eqType, glang.IdentExpr("$sw")),
			Op: glang.OpEquals,
			Y:  ctx.handleImplicitConversion(c.List[0], t, eqType, ctx.expr(c.List[0])),
		}

		for _, y := range c.List[1:] {
			t := ctx.typeOf(y)
			eqType := ctx.typeJoin(y, t, tagType)
			cond = glang.BinaryExpr{
				Op: glang.OpLOr,
				X: glang.BinaryExpr{
					X:  ctx.handleImplicitConversion(y, tagType, eqType, glang.IdentExpr("$sw")),
					Op: glang.OpEquals,
					Y:  ctx.handleImplicitConversion(y, t, eqType, ctx.expr(y)),
				},
				Y: cond,
			}
		}

		e = glang.IfExpr{
			Cond: cond,
			Then: ctx.stmtList(c.Body, nil),
			Else: e,
		}
	}

	e = glang.LetExpr{Names: []string{"$sw"}, ValExpr: tagExpr, Cont: e}

	if s.Init != nil {
		e = glang.ParenExpr{Inner: ctx.stmt(s.Init, e)}
	}

	e = glang.SeqExpr{Expr: e, Cont: cont}
	return
}

func (ctx Ctx) ifStmt(s *ast.IfStmt, cont glang.Expr) glang.Expr {
	var elseExpr glang.Expr = glang.DoExpr{Expr: glang.Tt}
	if s.Else != nil {
		elseExpr = ctx.stmt(s.Else, nil)
	}
	var ife glang.Expr = glang.IfExpr{
		Cond: ctx.handleImplicitConversion(s.Cond, ctx.typeOf(s.Cond), types.Typ[types.Bool], ctx.expr(s.Cond)),
		Then: ctx.blockStmt(s.Body, nil),
		Else: elseExpr,
	}

	if s.Init != nil {
		ife = glang.ParenExpr{Inner: ctx.stmt(s.Init, ife)}
	}
	return glang.SeqExpr{Expr: ife, Cont: cont}
}

func (ctx Ctx) forStmt(s *ast.ForStmt, cont glang.Expr) glang.Expr {
	var cond glang.Expr = glang.True
	if s.Cond != nil {
		cond = ctx.expr(s.Cond)
	}
	var post glang.Expr = glang.Skip
	if s.Post != nil {
		post = ctx.stmt(s.Post, nil)
	}

	body := ctx.blockStmt(s.Body, nil)
	var e glang.Expr = glang.ForLoopExpr{
		Cond: cond,
		Post: post,
		Body: body,
	}
	if s.Init != nil {
		e = glang.ParenExpr{Inner: ctx.stmt(s.Init, e)}
	}
	return glang.SeqExpr{Expr: e, Cont: cont}
}

func getIdentOrAnonymous(e ast.Expr) (ident string, ok bool) {
	if e == nil {
		return "_", true
	}
	return getIdent(e)
}

func (ctx Ctx) mapRangeStmt(s *ast.RangeStmt) glang.Expr {
	key, ok := getIdentOrAnonymous(s.Key)
	if !ok {
		ctx.nope(s.Key, "range with non-ident key")
		return nil
	}
	val, ok := getIdentOrAnonymous(s.Value)
	if !ok {
		ctx.nope(s.Value, "range with non-ident value")
		return nil
	}
	return glang.ForRangeMapExpr{
		KeyIdent:   key,
		ValueIdent: val,
		Map:        ctx.expr(s.X),
		Body:       ctx.blockStmt(s.Body, nil),
	}
}

func (ctx Ctx) identBinder(id *ast.Ident) glang.Binder {
	if id == nil {
		return glang.Binder(nil)
	}
	e := glang.IdentExpr(id.Name)
	return &e
}

func (ctx Ctx) sliceRangeStmt(s *ast.RangeStmt) glang.Expr {
	if s.Tok != token.DEFINE {
		ctx.unsupported(s.Key, "range with pre-existing variables")
	}
	key, ok := s.Key.(*ast.Ident)
	if !ok {
		ctx.todo(s.Key, "range with non-identifier as iteration variable")
	}
	valExpr := glang.Binder(nil)
	if s.Value != nil {
		val, ok := s.Value.(*ast.Ident)
		if !ok {
			ctx.todo(s.Value, "range with non-identifier as iteration variable")
		}
		valExpr = ctx.identBinder(val)
	}

	var e glang.Expr = glang.ForRangeSliceExpr{
		Key:   ctx.identBinder(key),
		Val:   valExpr,
		Slice: glang.IdentExpr("$range"),
		Ty:    ctx.glangType(s.X, sliceElem(ctx.typeOf(s.X))),
		Body:  ctx.blockStmt(s.Body, nil),
	}
	return glang.LetExpr{
		Names:   []string{"$range"},
		ValExpr: ctx.expr(s.X),
		Cont:    e,
	}
}

func (ctx Ctx) rangeStmt(s *ast.RangeStmt) glang.Expr {
	switch ctx.typeOf(s.X).Underlying().(type) {
	case *types.Map:
		return ctx.mapRangeStmt(s)
	case *types.Slice:
		return ctx.sliceRangeStmt(s)
	default:
		ctx.unsupported(s,
			"range over %v (only maps and slices are supported)",
			ctx.typeOf(s.X).Underlying())
		return nil
	}
}

func (ctx Ctx) defineStmt(s *ast.AssignStmt, cont glang.Expr) glang.Expr {
	e := ctx.assignStmt(s, cont)

	// Before the asignStmt "e", allocate everything that's new in this define stmt.
	for _, lhsExpr := range s.Lhs {
		if ident, ok := lhsExpr.(*ast.Ident); ok {
			if _, ok := ctx.info.Defs[ident]; ok { // if this identifier is defining something
				t := ctx.glangType(ident, ctx.info.TypeOf(ident))
				e = glang.LetExpr{
					Names: []string{ident.Name},
					ValExpr: glang.RefExpr{
						X:  glang.NewCallExpr(glang.GallinaIdent("zero_val"), t),
						Ty: t,
					},
					Cont: e,
				}
			}
		} else {
			ctx.nope(lhsExpr, "defining a non-identifier")
		}
	}

	return e
}

func (ctx Ctx) varSpec(s *ast.ValueSpec, cont glang.Expr) glang.Expr {
	var lhs []ast.Expr
	for _, l := range s.Names {
		lhs = append(lhs, l)
	}
	return ctx.defineStmt(&ast.AssignStmt{Lhs: lhs, Rhs: s.Values}, cont)
}

// varDeclStmt translates declarations within functions
func (ctx Ctx) varDeclStmt(s *ast.DeclStmt, cont glang.Expr) glang.Expr {
	decl, ok := s.Decl.(*ast.GenDecl)
	if !ok {
		ctx.noExample(s, "declaration that is not a GenDecl")
	}
	if decl.Tok != token.VAR {
		ctx.unsupported(s, "non-var declaration for %v", decl.Tok)
	}
	var expr glang.Expr = cont
	for _, spec := range decl.Specs {
		// guaranteed to be a *Ast.ValueSpec due to decl.Tok
		//
		// https://golang.org/pkg/go/ast/#GenDecl
		// TODO: handle TypeSpec
		expr = ctx.varSpec(spec.(*ast.ValueSpec), expr)
	}
	return expr
}

// Returns the address of the given expression.
// Requires that e is actually addressable
func (ctx Ctx) exprAddr(e ast.Expr) glang.Expr {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return ctx.exprAddr(e.X)
	case *ast.Ident:
		return glang.IdentExpr(e.Name)
	case *ast.IndexExpr:
		targetTy := ctx.typeOf(e.X)
		switch targetTy := targetTy.Underlying().(type) {
		case *types.Slice:
			return glang.NewCallExpr(glang.GallinaIdent("slice.elem_ref"),
				ctx.glangType(e, targetTy.Elem()),
				ctx.expr(e.X),
				ctx.expr(e.Index))
		case *types.Map:
			ctx.nope(e, "map index expressions are not addressable")
		case *types.Array:
			return glang.NewCallExpr(glang.GallinaIdent("array.elem_ref"),
				ctx.glangType(e, targetTy.Elem()), ctx.expr(e.X), ctx.expr(e.Index))
		default:
			ctx.unsupported(e, "index addr to unexpected target of type %v", targetTy)
		}
	case *ast.StarExpr:
		return ctx.expr(e.X)
	case *ast.SelectorExpr:
		return ctx.selectorExprAddr(e)
	default:
		ctx.unsupported(e, "address of unknown expression")
	}
	return nil
}

func (ctx Ctx) assignFromTo(lhs ast.Expr, rhs glang.Expr, cont glang.Expr) glang.Expr {
	// lhs should either be a map index expression, or is addressable
	switch lhs := lhs.(type) {
	case *ast.Ident:
		if lhs.Name == "_" {
			return glang.NewDoSeq(rhs, cont)
		}
	case *ast.IndexExpr:
		targetTy := ctx.typeOf(lhs.X).Underlying()
		switch targetTy.(type) {
		case *types.Map:
			return glang.NewDoSeq(glang.NewCallExpr(glang.GallinaIdent("map.insert"),
				ctx.expr(lhs.X),
				ctx.expr(lhs.Index),
				rhs), cont)
		}
	}

	return glang.NewDoSeq(glang.StoreStmt{
		Dst: ctx.exprAddr(lhs),
		X:   rhs,
		Ty:  ctx.glangType(lhs, ctx.typeOf(lhs)),
	}, cont)
}

// This handles conversions arising from the notion of "assignability" in the Go spec.
func (ctx Ctx) handleImplicitConversion(n ast.Node, from, to types.Type, e glang.Expr) glang.Expr {
	if to == nil {
		// This happens when the LHS is `_`
		return e
	}
	if from == nil {
		ctx.unsupported(n, "implicit conversion: don't know from type")
	}
	fromUnder := from.Underlying()
	toUnder := to.Underlying()
	if types.Identical(fromUnder, toUnder) {
		return e
	}
	if _, ok := toUnder.(*types.Interface); ok {
		if _, ok := from.(*types.Interface); ok {
			// if both are interface types, then no need to convert anything
			// because the GooseLang representation of interface values is
			// independent of the particular interface type.
			return e
		}

		maybePtrSuffix := ""
		if fromPointer, ok := from.(*types.Pointer); ok {
			from = fromPointer.Elem()
			maybePtrSuffix = "_ptr"
		}
		if fromNamed, ok := from.(*types.Named); ok {
			msetName := ctx.qualifiedName(fromNamed.Obj()) + "__mset" + maybePtrSuffix
			ctx.dep.addDep(msetName)
			return glang.NewCallExpr(glang.GallinaIdent("interface.make"), glang.GallinaIdent(msetName), e)
		} else if fromBasic, ok := from.(*types.Basic); ok {
			msetName := fromBasic.Name() + "__mset" + maybePtrSuffix
			ctx.dep.addDep(msetName)
			return glang.NewCallExpr(glang.GallinaIdent("interface.make"), glang.GallinaIdent(msetName), e)
		}
	}
	if fromBasic, ok := fromUnder.(*types.Basic); ok && fromBasic.Kind() == types.UntypedNil {
		if _, ok := toUnder.(*types.Slice); ok {
			return glang.GallinaIdent("slice.nil")
		} else if _, ok := toUnder.(*types.Pointer); ok {
			return glang.GallinaIdent("#null")
		} else if _, ok := toUnder.(*types.Chan); ok {
			return glang.GallinaIdent("chan.nil")
		} else if _, ok := toUnder.(*types.Map); ok {
			return glang.GallinaIdent("map.nil")
		} else if _, ok := toUnder.(*types.Signature); ok {
			return glang.GallinaIdent("nil")
		}
	}

	if fromBasic, ok := fromUnder.(*types.Basic); ok && fromBasic.Kind() == types.UntypedBool {
		if toBasic, ok := toUnder.(*types.Basic); ok && toBasic.Kind() == types.Bool {
			// XXX: not making a distinction between typed and untyped bool.
			// Untyped bool are the runtime result of comparison operators,
			// whereas other untyped types are only used in constants.
			return e
		}
	}

	if fromBasic, ok := fromUnder.(*types.Basic); ok && fromBasic.Kind() == types.UntypedString {
		if toBasic, ok := toUnder.(*types.Basic); ok && toBasic.Kind() == types.String {
			return glang.StringVal{Value: e}
		}
	}

	if fromBasic, ok := fromUnder.(*types.Basic); ok && fromBasic.Kind() == types.UntypedInt {
		if toBasic, ok := toUnder.(*types.Basic); ok {
			switch toBasic.Kind() {
			case types.Uint64, types.Int64, types.Int:
				// XXX: treat an `int` as a `int64`
				return glang.Int64Val{Value: e}
			case types.Uint32, types.Int32:
				return glang.Int32Val{Value: e}
			case types.Uint8, types.Int8:
				return glang.Int8Val{Value: e}
			}
		}
	}

	fromChan, ok1 := fromUnder.(*types.Chan)
	toChan, ok2 := toUnder.(*types.Chan)
	if ok1 && ok2 {
		if types.Identical(fromChan.Elem(), toChan.Elem()) {
			return e
		}
	}

	ctx.unsupported(n, "implicit conversion from %s to %s", from, to)
	panic("unreachable")
}

func (ctx Ctx) assignStmt(s *ast.AssignStmt, cont glang.Expr) glang.Expr {
	e := cont
	if len(s.Rhs) == 0 {
		return e
	}

	// Determine RHS types, specially handling multiple returns from a function call.
	var rhsTypes []types.Type
	for i := 0; i < len(s.Rhs); i++ {
		t := ctx.typeOf(s.Rhs[i])
		if tuple, ok := t.(*types.Tuple); ok {
			for i := range tuple.Len() {
				rhsTypes = append(rhsTypes, tuple.At(i).Type())
			}
		} else {
			rhsTypes = append(rhsTypes, t)
		}
	}

	// collect the RHS expressions
	var rhsExprs []glang.Expr
	if len(s.Rhs) == len(s.Lhs) {
		for _, rhs := range s.Rhs {
			rhsExprs = append(rhsExprs, ctx.expr(rhs))
		}
	} else {
		// RHS is a function call returning multiple things. Will introduce
		// extra let bindings to destructure those multiple returns.
		for i := range s.Lhs {
			rhsExprs = append(rhsExprs, glang.IdentExpr(fmt.Sprintf("$ret%d", i)))
		}
	}

	// Execute assignments left-to-right
	for i := len(s.Lhs); i > 0; i-- {
		e = ctx.assignFromTo(s.Lhs[i-1], glang.IdentExpr(fmt.Sprintf("$r%d", i-1)), e)
	}

	// Let bindings for RHSs including conversions
	for i := len(s.Lhs); i > 0; i-- {
		e = glang.LetExpr{
			Names: []string{fmt.Sprintf("$r%d", i-1)},
			ValExpr: ctx.handleImplicitConversion(s.Lhs[i-1], rhsTypes[i-1],
				ctx.typeOf(s.Lhs[i-1]), rhsExprs[i-1]),
			Cont: e,
		}
	}

	// Extra let bindings in case RHS is a multiple-returning function
	if len(s.Rhs) != len(s.Lhs) && len(s.Lhs) > 0 {
		var n []string
		for i := range s.Lhs {
			n = append(n, fmt.Sprintf("$ret%d", i))
		}
		e = glang.LetExpr{
			Names:   n,
			ValExpr: ctx.exprSpecial(s.Rhs[0], true),
			Cont:    e,
		}
	}

	return e
}

func (ctx Ctx) assignOpStmt(s *ast.AssignStmt, cont glang.Expr) glang.Expr {
	assignOps := map[token.Token]glang.BinOp{
		token.ADD_ASSIGN: glang.OpPlus,
		token.SUB_ASSIGN: glang.OpMinus,
	}
	op, ok := assignOps[s.Tok]
	if !ok {
		ctx.unsupported(s, "unsupported assign+update operation %v", s.Tok)
	}
	rhs := glang.BinaryExpr{
		X:  ctx.expr(s.Lhs[0]),
		Op: op,
		Y:  ctx.expr(s.Rhs[0]),
	}
	return ctx.assignFromTo(s.Lhs[0], rhs, cont)
}

func (ctx Ctx) incDecStmt(stmt *ast.IncDecStmt, cont glang.Expr) glang.Expr {
	op := glang.OpPlus
	if stmt.Tok == token.DEC {
		op = glang.OpMinus
	}
	return ctx.assignFromTo(stmt.X, glang.BinaryExpr{
		X:  ctx.expr(stmt.X),
		Op: op,
		Y:  glang.Int64Val{Value: glang.ZLiteral{Value: big.NewInt(1)}},
	}, cont)
}

func (ctx Ctx) branchStmt(s *ast.BranchStmt, cont glang.Expr) glang.Expr {
	if s.Tok == token.CONTINUE {
		return glang.SeqExpr{Expr: glang.ContinueExpr{}, Cont: cont}
	}
	if s.Tok == token.BREAK {
		return glang.SeqExpr{Expr: glang.BreakExpr{}, Cont: cont}
	}
	ctx.noExample(s, "unexpected control flow %v in loop", s.Tok)
	return nil
}

func (ctx Ctx) goStmt(e *ast.GoStmt, cont glang.Expr) glang.Expr {
	args := make([]glang.Expr, 0, len(e.Call.Args))
	for i := range len(e.Call.Args) {
		args = append(args, glang.IdentExpr(fmt.Sprintf("a%d", i)))
	}
	var expr glang.Expr = glang.NewDoSeq(glang.SpawnExpr{Body: glang.NewCallExpr(
		glang.IdentExpr("$go"),
		args...,
	)}, cont)

	expr = glang.LetExpr{
		Names:   []string{"$go"},
		ValExpr: ctx.expr(e.Call.Fun),
		Cont:    expr,
	}

	expr = ctx.callExprPrelude(e.Call, expr)

	return expr
}

func (ctx Ctx) returnStmt(s *ast.ReturnStmt, cont glang.Expr) glang.Expr {
	exprs := make([]glang.Expr, 0, len(s.Results))
	var expectedReturnTypes []types.Type
	if ctx.curFuncType.Results() != nil {
		for i := range ctx.curFuncType.Results().Len() {

			expectedReturnTypes = append(expectedReturnTypes, ctx.curFuncType.Results().At(i).Type())
		}
	}

	var retExpr glang.Expr
	func() {
		var unconvertedReturnValues []exprWithInfo
		if len(s.Results) == 1 && len(expectedReturnTypes) > 1 {
			// special case
			tupleType, ok := ctx.typeOf(s.Results[0]).(*types.Tuple)
			if !ok {
				ctx.nope(s.Results[0], "the only way for the number of values in a returnStmt to mismatch "+
					"the function's signature is passing through a multiple-returning function")
			}
			for i := range tupleType.Len() {
				unconvertedReturnValues = append(unconvertedReturnValues, exprWithInfo{
					e: glang.IdentExpr(fmt.Sprintf("$ret%d", i)),
					t: tupleType.At(i).Type(),
					n: s.Results[0],
				})
			}
			defer func() {
				var names []string
				for i := range tupleType.Len() {
					names = append(names, fmt.Sprintf("$ret%d", i))
				}
				retExpr = glang.LetExpr{Names: names,
					ValExpr: glang.ParenExpr{Inner: ctx.expr(s.Results[0])},
					Cont:    retExpr,
				}
			}()
		} else {
			for _, result := range s.Results {
				unconvertedReturnValues = append(unconvertedReturnValues, exprWithInfo{
					e: ctx.expr(result),
					t: ctx.typeOf(result),
					n: result,
				})
			}
		}

		for i, e := range unconvertedReturnValues {
			exprs = append(exprs, ctx.handleImplicitConversion(e.n, e.t, expectedReturnTypes[i], e.e))
		}
		if len(exprs) == 0 { // return #()
			exprs = []glang.Expr{glang.Tt}
		}
		retExpr = glang.ReturnExpr{Value: glang.TupleExpr(exprs)}
	}()

	return glang.SeqExpr{Expr: retExpr, Cont: cont}
}

func (ctx Ctx) deferStmt(s *ast.DeferStmt, cont glang.Expr) (expr glang.Expr) {
	*ctx.usesDefer = true
	args := make([]glang.Expr, 0, len(s.Call.Args))
	for i := range len(s.Call.Args) {
		args = append(args, glang.IdentExpr(fmt.Sprintf("a%d", i)))
	}

	expr = glang.LetExpr{
		ValExpr: glang.NewCallExpr(glang.IdentExpr("$f"), args...),
		Cont:    glang.NewCallExpr(glang.IdentExpr("$oldf"), glang.Tt),
	}

	expr = glang.FuncLit{Body: expr}

	expr = glang.LetExpr{
		Names:   []string{"$oldf"},
		ValExpr: glang.DerefExpr{X: glang.IdentExpr("$defer"), Ty: glang.FuncType{}},
		Cont:    expr,
	}

	expr = glang.StoreStmt{
		Dst: glang.IdentExpr("$defer"),
		Ty:  glang.FuncType{},
		X:   expr,
	}

	expr = glang.LetExpr{
		Names:   []string{"$f"},
		ValExpr: ctx.expr(s.Call.Fun),
		Cont:    expr,
	}

	expr = ctx.callExprPrelude(s.Call, expr)
	expr = glang.NewDoSeq(expr, cont)
	return
}

func (ctx Ctx) selectStmt(s *ast.SelectStmt, cont glang.Expr) (expr glang.Expr) {
	var sends glang.ListExpr
	var recvs glang.ListExpr
	var def glang.Expr = glang.NewCallExpr(glang.GallinaIdent("InjLV"), glang.Tt)

	// build up select statement itself
	for _, s := range s.Body.List {
		s := s.(*ast.CommClause)
		if s.Comm == nil {
			def = glang.NewCallExpr(glang.GallinaIdent("InjR"), glang.FuncLit{Body: ctx.stmtList(s.Body, nil)})
		} else if _, ok := s.Comm.(*ast.SendStmt); ok {
			sendIndex := len(sends)
			sends = append(sends, glang.TupleExpr([]glang.Expr{
				glang.IdentExpr(fmt.Sprintf("$sendVal%d", sendIndex)),
				glang.IdentExpr(fmt.Sprintf("$sendChan%d", sendIndex)),
				glang.FuncLit{Body: ctx.stmtList(s.Body, nil)},
			}))
		} else { // must be a receive stmt
			recvIndex := len(recvs)
			body := ctx.stmtList(s.Body, nil)

			// want to figure out the first statment to run in the body
			switch comm := s.Comm.(type) {
			case *ast.ExprStmt:
				recvExpr := comm.X.(*ast.UnaryExpr)
				if recvExpr.Op != token.ARROW {
					ctx.nope(comm.X, "expected recv statement")
				}
				// nothing to run in the body
			case *ast.AssignStmt:
				// XXX: replace the RHS in the assignment statement with an
				// ident, so we can put this straight in the the body list
				if len(comm.Rhs) != 1 {
					ctx.unsupported(comm, "expected a single receive operation on RHS")
				}
				var rhs ast.Expr = comm.Rhs[0]
				for {
					if r, ok := rhs.(*ast.ParenExpr); ok {
						rhs = r.X
					} else {
						break
					}
				}
				recvExpr := rhs.(*ast.UnaryExpr)
				if recvExpr.Op != token.ARROW {
					ctx.nope(comm.Rhs[0], "expected recv statement")
				}

				// XXX: create a new AST node and enough typing information for
				// an assignStmt to translate.
				t := ctx.info.Types[recvExpr]
				comm.Rhs[0] = ast.NewIdent("$recvVal")
				ctx.info.Types[comm.Rhs[0]] = t

				if comm.Tok == token.DEFINE {
					body = ctx.defineStmt(comm, body)
				} else if comm.Tok == token.ASSIGN {
					body = ctx.assignStmt(comm, body)
				}
			default:
				ctx.unsupported(s.Comm, "unexpected statement in select clause")
			}

			recvs = append(recvs, glang.TupleExpr([]glang.Expr{
				glang.IdentExpr(fmt.Sprintf("$recvChan%d", recvIndex)),
				glang.FuncLit{Args: []glang.FieldDecl{{Name: "$recvVal"}}, Body: body},
			}))
		}
	}

	expr = glang.NewCallExpr(glang.GallinaIdent("chan.select"), sends, recvs, def)
	expr = glang.NewDoSeq(expr, cont)
	return
}

func (ctx Ctx) sendStmt(s *ast.SendStmt, cont glang.Expr) (expr glang.Expr) {
	expr = glang.NewCallExpr(glang.GallinaIdent("chan.send"), glang.IdentExpr("$chan"), glang.IdentExpr("$v"))
	// XXX: left-to-right evaluation, might not match Go
	expr = glang.LetExpr{Names: []string{"$v"}, ValExpr: ctx.expr(s.Value), Cont: expr}
	expr = glang.LetExpr{Names: []string{"$chan"}, ValExpr: ctx.expr(s.Chan), Cont: expr}
	expr = glang.NewDoSeq(expr, cont)
	return
}

func (ctx Ctx) stmt(s ast.Stmt, cont glang.Expr) glang.Expr {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return ctx.returnStmt(s, cont)
	case *ast.BranchStmt:
		return ctx.branchStmt(s, cont)
	case *ast.IfStmt:
		return ctx.ifStmt(s, cont)
	case *ast.GoStmt:
		return ctx.goStmt(s, cont)
	case *ast.ExprStmt:
		return glang.NewDoSeq(ctx.expr(s.X), cont)
	case *ast.AssignStmt:
		if s.Tok == token.DEFINE {
			return ctx.defineStmt(s, cont)
		} else if s.Tok == token.ASSIGN {
			return ctx.assignStmt(s, cont)
		} else {
			return ctx.assignOpStmt(s, cont)
		}
	case *ast.DeclStmt:
		return ctx.varDeclStmt(s, cont)
	case *ast.IncDecStmt:
		return ctx.incDecStmt(s, cont)
	case *ast.ForStmt:
		// note that this might be a nested loop,
		// in which case the loop var gets replaced by the inner loop.
		return ctx.forStmt(s, cont)
	case *ast.RangeStmt:
		return glang.NewDoSeq(ctx.rangeStmt(s), cont)
	case *ast.BlockStmt:
		return ctx.blockStmt(s, cont)
	case *ast.SwitchStmt:
		return ctx.switchStmt(s, cont)
	case *ast.TypeSwitchStmt:
		ctx.todo(s, "type switch statement")
	case *ast.DeferStmt:
		return ctx.deferStmt(s, cont)
	case *ast.SelectStmt:
		return ctx.selectStmt(s, cont)
	case *ast.SendStmt:
		return ctx.sendStmt(s, cont)
	default:
		ctx.unsupported(s, "statement %T", s)
	}
	panic("unreachable")
}

// returnType converts an Ast.FuncType's Results to a Coq return type
func (ctx Ctx) returnType(results *ast.FieldList) glang.Type {
	if results == nil {
		return glang.TypeIdent("unitT")
	}
	rs := results.List
	for _, r := range rs {
		if len(r.Names) > 0 {
			ctx.unsupported(r, "named returned value")
			return glang.TypeIdent("<invalid>")
		}
	}
	var ts []glang.Type
	for _, r := range rs {
		if len(r.Names) > 0 {
			ctx.unsupported(r, "named returned value")
			return glang.TypeIdent("<invalid>")
		}
		ts = append(ts, ctx.glangTypeFromExpr(r.Type))
	}
	return glang.NewTupleType(ts)
}

func (ctx Ctx) funcDecl(d *ast.FuncDecl) glang.FuncDecl {
	ctx.usesDefer = new(bool)

	fd := glang.FuncDecl{Name: d.Name.Name}
	addSourceDoc(d.Doc, &fd.Comment)
	ctx.addSourceFile(d, &fd.Comment)
	fd.TypeParams = ctx.typeParamList(d.Type.TypeParams)
	if d.Recv != nil {
		if len(d.Recv.List) != 1 {
			ctx.nope(d, "function with multiple receivers")
		}
		receiver := d.Recv.List[0]
		recvType := types.Unalias(ctx.typeOf(receiver.Type))
		// recvType must be either a "named" type or a pointer type that points to a named type.
		if baseType, ok := recvType.(*types.Pointer); ok {
			recvType = baseType.Elem()
		}
		namedType, ok := recvType.(*types.Named)
		if !ok {
			ctx.nope(d.Recv, "expected named type as method receiver, got %T", recvType)
		}
		typeName := namedType.Obj().Name()

		fd.Name = glang.TypeMethod(typeName, d.Name.Name)
		f := ctx.field(receiver)
		fd.RecvArg = &f
	}
	ctx.dep.setCurrentName(fd.Name)
	defer ctx.dep.unsetCurrentName()

	fd.Args = append(fd.Args, ctx.paramList(d.Type.Params)...)

	fd.ReturnType = ctx.returnType(d.Type.Results)

	fd.Body = ctx.blockStmt(d.Body, nil)
	for _, arg := range fd.Args {
		fd.Body = glang.LetExpr{
			Names:   []string{arg.Name},
			ValExpr: glang.RefExpr{Ty: arg.Type, X: glang.IdentExpr(arg.Name)},
			Cont:    fd.Body,
		}
	}
	if fd.RecvArg != nil {
		fd.Body = glang.LetExpr{
			Names:   []string{fd.RecvArg.Name},
			ValExpr: glang.RefExpr{Ty: fd.RecvArg.Type, X: glang.IdentExpr(fd.RecvArg.Name)},
			Cont:    fd.Body,
		}
	}

	if *ctx.usesDefer {
		fd.Body = glang.NewCallExpr(glang.GallinaIdent("with_defer:"), fd.Body)
	} else {
		fd.Body = glang.NewCallExpr(glang.GallinaIdent("exception_do"), fd.Body)
	}

	return fd
}

// this should only be used for untyped constant literals
func (ctx Ctx) constantLiteral(l locatable, v constant.Value) (types.Type, glang.Expr) {
	switch v.Kind() {
	case constant.Bool:
		return types.Typ[types.UntypedBool], glang.BoolLiteral(constant.Val(v).(bool))
	case constant.String:
		return types.Typ[types.UntypedString], glang.StringLiteral{Value: constant.Val(v).(string)}
	case constant.Int:
		switch v := constant.Val(v).(type) {
		case *big.Int:
			return types.Typ[types.UntypedInt], glang.ZLiteral{Value: v}
		case int64:
			return types.Typ[types.UntypedInt], glang.ZLiteral{Value: big.NewInt(v)}
		default:
			ctx.nope(l, "untyped int const with unexpected type")
		}
	}
	ctx.unsupported(l, "unsupported constant val")
	return nil, nil
}

func (ctx Ctx) declType(t types.Type) glang.Expr {
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.UntypedBool:
			return glang.GallinaIdent("bool")
		case types.UntypedString:
			return glang.GallinaIdent("string")
		case types.UntypedInt:
			return glang.GallinaIdent("Z")
		}
	}
	return glang.GallinaIdent("expr")
}

func (ctx Ctx) constSpec(spec *ast.ValueSpec) []glang.Decl {
	var cds []glang.Decl
	for i := range spec.Names {
		cd := glang.ConstDecl{
			Name: spec.Names[i].Name,
		}
		ctx.dep.setCurrentName(cd.Name)
		defer ctx.dep.unsetCurrentName()

		addSourceDoc(spec.Comment, &cd.Comment)
		if len(spec.Values) > 0 {
			cd.Val =
				ctx.handleImplicitConversion(spec.Names[i],
					ctx.typeOf(spec.Values[i]), ctx.typeOf(spec.Names[i]),
					ctx.expr(spec.Values[i]))
		} else {
			fromT, v := ctx.constantLiteral(spec.Names[i], ctx.info.ObjectOf(spec.Names[i]).(*types.Const).Val())
			cd.Val = ctx.handleImplicitConversion(spec.Names[i], fromT, ctx.typeOf(spec.Names[i]), v)
		}
		cd.Type = ctx.declType(ctx.typeOf(spec.Names[i]))
		cds = append(cds, cd)
	}
	return cds
}

func (ctx Ctx) constDecl(d *ast.GenDecl) []glang.Decl {
	var specs []glang.Decl
	for _, spec := range d.Specs {
		spec := spec.(*ast.ValueSpec)
		specs = append(specs, ctx.constSpec(spec)...)
	}
	return specs
}

func (ctx Ctx) globalVarDecl(d *ast.GenDecl) []glang.Decl {
	ctx.unsupported(d, "global vars")
	return nil
}

func stringLitValue(lit *ast.BasicLit) string {
	if lit.Kind != token.STRING {
		panic("unexpected non-string literal")
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		panic("unexpected string literal value: " + err.Error())
	}
	return s
}

var ffiMapping = map[string]string{
	"github.com/mit-pdos/gokv/grove_ffi":         "grove",
	"github.com/goose-lang/primitive/disk":       "disk",
	"github.com/goose-lang/primitive/async_disk": "async_disk",
}

func (ctx Ctx) imports(d []ast.Spec) []glang.Decl {
	var decls []glang.Decl
	for _, s := range d {
		s := s.(*ast.ImportSpec)
		if s.Name != nil {
			ctx.unsupported(s, "renaming imports")
		}
		importPath := stringLitValue(s.Path)
		// TODO: this uses the syntax of the Go import to determine the Coq
		// import, but Go packages can contain a different name than their
		// path. We can get this information by using the *types.Package
		// returned by Check (or the pkg.Types field from *packages.Package).
		decls = append(decls, glang.ImportDecl{Path: importPath})
	}
	return decls
}

func (ctx Ctx) maybeDecls(d ast.Decl) []glang.Decl {
	switch d := d.(type) {
	case *ast.FuncDecl:
		ctx.curFuncType = ctx.typeOf(d.Name).(*types.Signature)
		fd := ctx.funcDecl(d)
		ctx.curFuncType = nil
		return []glang.Decl{fd}
	case *ast.GenDecl:
		switch d.Tok {
		case token.IMPORT:
			return ctx.imports(d.Specs)
		case token.CONST:
			return ctx.constDecl(d)
		case token.VAR:
			return ctx.globalVarDecl(d)
		case token.TYPE:
			if len(d.Specs) > 1 {
				ctx.noExample(d, "multiple specs in a type decl")
			}
			spec := d.Specs[0].(*ast.TypeSpec)
			return ctx.typeDecl(spec)
		default:
			ctx.nope(d, "unknown token type in decl")
		}
	case *ast.BadDecl:
		ctx.nope(d, "bad declaration in type-checked code")
	default:
		ctx.nope(d, "top-level decl")
	}
	return nil
}
