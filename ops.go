package goose

import (
	"github.com/goose-lang/goose/glang"
	"go/token"
)

var generalOps = map[token.Token]glang.BinOp{
	token.EQL: glang.OpEquals,
	token.NEQ: glang.OpNotEquals,
}

var untypedIntOps = map[token.Token]glang.BinOp{
	token.ADD: glang.OpPlus,
	token.LSS: glang.OpLessThanZ,
	token.GTR: glang.OpGreaterThanZ,
	token.SUB: glang.OpMinus,
	token.EQL: glang.OpEqualsZ,
	token.NEQ: glang.OpNotEquals,
	token.MUL: glang.OpMul,
	token.QUO: glang.OpQuot,
	token.REM: glang.OpRem,
	token.LEQ: glang.OpLessEqZ,
	token.GEQ: glang.OpGreaterEqZ,
}

var unsignedIntOps = map[token.Token]glang.BinOp{
	token.ADD:  glang.OpPlus,
	token.LSS:  glang.OpLessThan,
	token.GTR:  glang.OpGreaterThan,
	token.SUB:  glang.OpMinus,
	token.EQL:  glang.OpEquals,
	token.NEQ:  glang.OpNotEquals,
	token.MUL:  glang.OpMul,
	token.QUO:  glang.OpQuot,
	token.REM:  glang.OpRem,
	token.LEQ:  glang.OpLessEq,
	token.GEQ:  glang.OpGreaterEq,
	token.AND:  glang.OpAnd,
	token.LAND: glang.OpLAnd,
	token.OR:   glang.OpOr,
	token.LOR:  glang.OpLOr,
	token.XOR:  glang.OpXor,
	token.SHL:  glang.OpShl,
	token.SHR:  glang.OpShr,
}

var signedIntOps = map[token.Token]glang.BinOp{
	token.ADD: glang.OpPlus,
	token.SUB: glang.OpMinus,
	token.EQL: glang.OpEquals,
	token.NEQ: glang.OpNotEquals,
	token.MUL: glang.OpMul,
	token.AND: glang.OpAnd,
	token.OR:  glang.OpOr,
	token.XOR: glang.OpXor,
}

var stringOps = map[token.Token]glang.BinOp{
	token.ADD: glang.OpAppend,
	token.LSS: glang.OpLessThan,
	token.GTR: glang.OpGreaterThan,
	token.LEQ: glang.OpLessEq,
	token.GEQ: glang.OpGreaterEq,
	token.EQL: glang.OpEquals,
	token.NEQ: glang.OpNotEquals,
}

var boolOps = map[token.Token]glang.BinOp{
	token.EQL:  glang.OpEquals,
	token.NEQ:  glang.OpNotEquals,
	token.LAND: glang.OpLAnd,
	token.LOR:  glang.OpLOr,
}
