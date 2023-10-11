
package excelize

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/cmplx"
	"math/rand"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/xuri/efp"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const (
	// Excel formula errors
	formulaErrorDIV         = "#DIV/0!"
	formulaErrorNAME        = "#NAME?"
	formulaErrorNA          = "#N/A"
	formulaErrorNUM         = "#NUM!"
	formulaErrorVALUE       = "#VALUE!"
	formulaErrorREF         = "#REF!"
	formulaErrorNULL        = "#NULL!"
	formulaErrorSPILL       = "#SPILL!"
	formulaErrorCALC        = "#CALC!"
	formulaErrorGETTINGDATA = "#GETTING_DATA"
	// Formula criteria condition enumeration
	_ byte = iota
	criteriaEq
	criteriaLe
	criteriaGe
	criteriaNe
	criteriaL
	criteriaG
	criteriaErr
	criteriaRegexp

	categoryWeightAndMass
	categoryDistance
	categoryTime
	categoryPressure
	categoryForce
	categoryEnergy
	categoryPower
	categoryMagnetism
	categoryTemperature
	categoryVolumeAndLiquidMeasure
	categoryArea
	categoryInformation
	categorySpeed

	matchModeExact      = 0
	matchModeMinGreater = 1
	matchModeMaxLess    = -1
	matchModeWildcard   = 2

	searchModeLinear        = 1
	searchModeReverseLinear = -1
	searchModeAscBinary     = 2
	searchModeDescBinary    = -2

	maxFinancialIterations = 128
	financialPrecision     = 1.0e-08
	// Date and time format regular expressions
	monthRe    = `((jan|january)|(feb|february)|(mar|march)|(apr|april)|(may)|(jun|june)|(jul|july)|(aug|august)|(sep|september)|(oct|october)|(nov|november)|(dec|december))`
	df1        = `(([0-9])+)/(([0-9])+)/(([0-9])+)`
	df2        = monthRe + ` (([0-9])+), (([0-9])+)`
	df3        = `(([0-9])+)-(([0-9])+)-(([0-9])+)`
	df4        = `(([0-9])+)-` + monthRe + `-(([0-9])+)`
	datePrefix = `^((` + df1 + `|` + df2 + `|` + df3 + `|` + df4 + `) )?`
	tfhh       = `(([0-9])+) (am|pm)`
	tfhhmm     = `(([0-9])+):(([0-9])+)( (am|pm))?`
	tfmmss     = `(([0-9])+):(([0-9])+\.([0-9])+)( (am|pm))?`
	tfhhmmss   = `(([0-9])+):(([0-9])+):(([0-9])+(\.([0-9])+)?)( (am|pm))?`
	timeSuffix = `( (` + tfhh + `|` + tfhhmm + `|` + tfmmss + `|` + tfhhmmss + `))?$`
)

var (
	// tokenPriority defined basic arithmetic operator priority
	tokenPriority = map[string]int{
		"^":  5,
		"*":  4,
		"/":  4,
		"+":  3,
		"-":  3,
		"&":  2,
		"=":  1,
		"<>": 1,
		"<":  1,
		"<=": 1,
		">":  1,
		">=": 1,
	}
	month2num = map[string]int{
		"january":   1,
		"february":  2,
		"march":     3,
		"april":     4,
		"may":       5,
		"june":      6,
		"july":      7,
		"august":    8,
		"september": 9,
		"october":   10,
		"november":  11,
		"december":  12,
		"jan":       1,
		"feb":       2,
		"mar":       3,
		"apr":       4,
		"jun":       6,
		"jul":       7,
		"aug":       8,
		"sep":       9,
		"oct":       10,
		"nov":       11,
		"dec":       12,
	}
	dateFormats = map[string]*regexp.Regexp{
		"mm/dd/yy":    regexp.MustCompile(`^` + df1 + timeSuffix),
		"mm dd, yy":   regexp.MustCompile(`^` + df2 + timeSuffix),
		"yy-mm-dd":    regexp.MustCompile(`^` + df3 + timeSuffix),
		"yy-mmStr-dd": regexp.MustCompile(`^` + df4 + timeSuffix),
	}
	timeFormats = map[string]*regexp.Regexp{
		"hh":       regexp.MustCompile(datePrefix + tfhh + `$`),
		"hh:mm":    regexp.MustCompile(datePrefix + tfhhmm + `$`),
		"mm:ss":    regexp.MustCompile(datePrefix + tfmmss + `$`),
		"hh:mm:ss": regexp.MustCompile(datePrefix + tfhhmmss + `$`),
	}
	dateOnlyFormats = []*regexp.Regexp{
		regexp.MustCompile(`^` + df1 + `$`),
		regexp.MustCompile(`^` + df2 + `$`),
		regexp.MustCompile(`^` + df3 + `$`),
		regexp.MustCompile(`^` + df4 + `$`),
	}
	addressFmtMaps = map[string]func(col, row int) (string, error){
		"1_TRUE": func(col, row int) (string, error) {
			return CoordinatesToCellName(col, row, true)
		},
		"1_FALSE": func(col, row int) (string, error) {
			return fmt.Sprintf("R%dC%d", row, col), nil
		},
		"2_TRUE": func(col, row int) (string, error) {
			column, err := ColumnNumberToName(col)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s$%d", column, row), nil
		},
		"2_FALSE": func(col, row int) (string, error) {
			return fmt.Sprintf("R%dC[%d]", row, col), nil
		},
		"3_TRUE": func(col, row int) (string, error) {
			column, err := ColumnNumberToName(col)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("$%s%d", column, row), nil
		},
		"3_FALSE": func(col, row int) (string, error) {
			return fmt.Sprintf("R[%d]C%d", row, col), nil
		},
		"4_TRUE": func(col, row int) (string, error) {
			return CoordinatesToCellName(col, row, false)
		},
		"4_FALSE": func(col, row int) (string, error) {
			return fmt.Sprintf("R[%d]C[%d]", row, col), nil
		},
	}
	formulaFormats = []*regexp.Regexp{
		regexp.MustCompile(`^(\d+)$`),
		regexp.MustCompile(`^=(.*)$`),
		regexp.MustCompile(`^<>(.*)$`),
		regexp.MustCompile(`^<=(.*)$`),
		regexp.MustCompile(`^>=(.*)$`),
		regexp.MustCompile(`^<(.*)$`),
		regexp.MustCompile(`^>(.*)$`),
	}
	formulaCriterias = []byte{
		criteriaEq,
		criteriaEq,
		criteriaNe,
		criteriaLe,
		criteriaGe,
		criteriaL,
		criteriaG,
	}
)

// calcContext defines the formula execution context.
type calcContext struct {
	mu                sync.Mutex
	entry             string
	maxCalcIterations uint
	iterations        map[string]uint
	iterationsCache   map[string]formulaArg
}

// cellRef defines the structure of a cell reference.
type cellRef struct {
	Col   int
	Row   int
	Sheet string
}

// cellRef defines the structure of a cell range.
type cellRange struct {
	From cellRef
	To   cellRef
}

// formulaCriteria defined formula criteria parser result.
type formulaCriteria struct {
	Type      byte
	Condition formulaArg
}

// ArgType is the type of formula argument type.
type ArgType byte

// Formula argument types enumeration.
const (
	ArgUnknown ArgType = iota
	ArgNumber
	ArgString
	ArgList
	ArgMatrix
	ArgError
	ArgEmpty
)

// formulaArg is the argument of a formula or function.
type formulaArg struct {
	SheetName            string
	Number               float64
	String               string
	List                 []formulaArg
	Matrix               [][]formulaArg
	Boolean              bool
	Error                string
	Type                 ArgType
	cellRefs, cellRanges *list.List
}

// Value returns a string data type of the formula argument.
func (fa formulaArg) Value() (value string) {
	switch fa.Type {
	case ArgNumber:
		if fa.Boolean {
			if fa.Number == 0 {
				return "FALSE"
			}
			return "TRUE"
		}
		return fmt.Sprintf("%g", fa.Number)
	case ArgString:
		return fa.String
	case ArgError:
		return fa.Error
	}
	return
}

// ToNumber returns a formula argument with number data type.
func (fa formulaArg) ToNumber() formulaArg {
	var n float64
	var err error
	switch fa.Type {
	case ArgString:
		n, err = strconv.ParseFloat(fa.String, 64)
		if err != nil {
			return newErrorFormulaArg(formulaErrorVALUE, err.Error())
		}
	case ArgNumber:
		n = fa.Number
	}
	return newNumberFormulaArg(n)
}

// ToBool returns a formula argument with boolean data type.
func (fa formulaArg) ToBool() formulaArg {
	var b bool
	var err error
	switch fa.Type {
	case ArgString:
		b, err = strconv.ParseBool(fa.String)
		if err != nil {
			return newErrorFormulaArg(formulaErrorVALUE, err.Error())
		}
	case ArgNumber:
		if fa.Number == 1 {
			b = true
		}
	}
	return newBoolFormulaArg(b)
}

// ToList returns a formula argument with array data type.
func (fa formulaArg) ToList() []formulaArg {
	switch fa.Type {
	case ArgMatrix:
		var args []formulaArg
		for _, row := range fa.Matrix {
			args = append(args, row...)
		}
		return args
	case ArgList:
		return fa.List
	case ArgNumber, ArgString, ArgError, ArgUnknown:
		return []formulaArg{fa}
	}
	return nil
}

// formulaFuncs is the type of the formula functions.
type formulaFuncs struct {
	f           *File
	ctx         *calcContext
	sheet, cell string
}

// CalcCellValue provides a function to get calculated cell value. This feature
// is currently in working processing. Iterative calculation, implicit
// intersection, explicit intersection, array formula, table formula and some
// other formulas are not supported currently.
//
// Supported formula functions:
//
//	ABS
//	ACCRINT
//	ACCRINTM
//	ACOS
//	ACOSH
//	ACOT
//	ACOTH
//	ADDRESS
//	AGGREGATE
//	AMORDEGRC
//	AMORLINC
//	AND
//	ARABIC
//	ARRAYTOTEXT
//	ASIN
//	ASINH
//	ATAN
//	ATAN2
//	ATANH
//	AVEDEV
//	AVERAGE
//	AVERAGEA
//	AVERAGEIF
//	AVERAGEIFS
//	BASE
//	BESSELI
//	BESSELJ
//	BESSELK
//	BESSELY
//	BETA.DIST
//	BETA.INV
//	BETADIST
//	BETAINV
//	BIN2DEC
//	BIN2HEX
//	BIN2OCT
//	BINOM.DIST
//	BINOM.DIST.RANGE
//	BINOM.INV
//	BINOMDIST
//	BITAND
//	BITLSHIFT
//	BITOR
//	BITRSHIFT
//	BITXOR
//	CEILING
//	CEILING.MATH
//	CEILING.PRECISE
//	CHAR
//	CHIDIST
//	CHIINV
//	CHISQ.DIST
//	CHISQ.DIST.RT
//	CHISQ.INV
//	CHISQ.INV.RT
//	CHISQ.TEST
//	CHITEST
//	CHOOSE
//	CLEAN
//	CODE
//	COLUMN
//	COLUMNS
//	COMBIN
//	COMBINA
//	COMPLEX
//	CONCAT
//	CONCATENATE
//	CONFIDENCE
//	CONFIDENCE.NORM
//	CONFIDENCE.T
//	CONVERT
//	CORREL
//	COS
//	COSH
//	COT
//	COTH
//	COUNT
//	COUNTA
//	COUNTBLANK
//	COUNTIF
//	COUNTIFS
//	COUPDAYBS
//	COUPDAYS
//	COUPDAYSNC
//	COUPNCD
//	COUPNUM
//	COUPPCD
//	COVAR
//	COVARIANCE.P
//	COVARIANCE.S
//	CRITBINOM
//	CSC
//	CSCH
//	CUMIPMT
//	CUMPRINC
//	DATE
//	DATEDIF
//	DATEVALUE
//	DAVERAGE
//	DAY
//	DAYS
//	DAYS360
//	DB
//	DCOUNT
//	DCOUNTA
//	DDB
//	DEC2BIN
//	DEC2HEX
//	DEC2OCT
//	DECIMAL
//	DEGREES
//	DELTA
//	DEVSQ
//	DGET
//	DISC
//	DMAX
//	DMIN
//	DOLLARDE
//	DOLLARFR
//	DPRODUCT
//	DSTDEV
//	DSTDEVP
//	DSUM
//	DURATION
//	DVAR
//	DVARP
//	EDATE
//	EFFECT
//	ENCODEURL
//	EOMONTH
//	ERF
//	ERF.PRECISE
//	ERFC
//	ERFC.PRECISE
//	ERROR.TYPE
//	EUROCONVERT
//	EVEN
//	EXACT
//	EXP
//	EXPON.DIST
//	EXPONDIST
//	F.DIST
//	F.DIST.RT
//	F.INV
//	F.INV.RT
//	F.TEST
//	FACT
//	FACTDOUBLE
//	FALSE
//	FDIST
//	FIND
//	FINDB
//	FINV
//	FISHER
//	FISHERINV
//	FIXED
//	FLOOR
//	FLOOR.MATH
//	FLOOR.PRECISE
//	FORECAST
//	FORECAST.LINEAR
//	FORMULATEXT
//	FREQUENCY
//	FTEST
//	FV
//	FVSCHEDULE
//	GAMMA
//	GAMMA.DIST
//	GAMMA.INV
//	GAMMADIST
//	GAMMAINV
//	GAMMALN
//	GAMMALN.PRECISE
//	GAUSS
//	GCD
//	GEOMEAN
//	GESTEP
//	GROWTH
//	HARMEAN
//	HEX2BIN
//	HEX2DEC
//	HEX2OCT
//	HLOOKUP
//	HOUR
//	HYPERLINK
//	HYPGEOM.DIST
//	HYPGEOMDIST
//	IF
//	IFERROR
//	IFNA
//	IFS
//	IMABS
//	IMAGINARY
//	IMARGUMENT
//	IMCONJUGATE
//	IMCOS
//	IMCOSH
//	IMCOT
//	IMCSC
//	IMCSCH
//	IMDIV
//	IMEXP
//	IMLN
//	IMLOG10
//	IMLOG2
//	IMPOWER
//	IMPRODUCT
//	IMREAL
//	IMSEC
//	IMSECH
//	IMSIN
//	IMSINH
//	IMSQRT
//	IMSUB
//	IMSUM
//	IMTAN
//	INDEX
//	INDIRECT
//	INT
//	INTERCEPT
//	INTRATE
//	IPMT
//	IRR
//	ISBLANK
//	ISERR
//	ISERROR
//	ISEVEN
//	ISFORMULA
//	ISLOGICAL
//	ISNA
//	ISNONTEXT
//	ISNUMBER
//	ISO.CEILING
//	ISODD
//	ISOWEEKNUM
//	ISPMT
//	ISREF
//	ISTEXT
//	KURT
//	LARGE
//	LCM
//	LEFT
//	LEFTB
//	LEN
//	LENB
//	LN
//	LOG
//	LOG10
//	LOGINV
//	LOGNORM.DIST
//	LOGNORM.INV
//	LOGNORMDIST
//	LOOKUP
//	LOWER
//	MATCH
//	MAX
//	MAXA
//	MAXIFS
//	MDETERM
//	MDURATION
//	MEDIAN
//	MID
//	MIDB
//	MIN
//	MINA
//	MINIFS
//	MINUTE
//	MINVERSE
//	MIRR
//	MMULT
//	MOD
//	MODE
//	MODE.MULT
//	MODE.SNGL
//	MONTH
//	MROUND
//	MULTINOMIAL
//	MUNIT
//	N
//	NA
//	NEGBINOM.DIST
//	NEGBINOMDIST
//	NETWORKDAYS
//	NETWORKDAYS.INTL
//	NOMINAL
//	NORM.DIST
//	NORM.INV
//	NORM.S.DIST
//	NORM.S.INV
//	NORMDIST
//	NORMINV
//	NORMSDIST
//	NORMSINV
//	NOT
//	NOW
//	NPER
//	NPV
//	OCT2BIN
//	OCT2DEC
//	OCT2HEX
//	ODD
//	ODDFPRICE
//	ODDFYIELD
//	ODDLPRICE
//	ODDLYIELD
//	OR
//	PDURATION
//	PEARSON
//	PERCENTILE
//	PERCENTILE.EXC
//	PERCENTILE.INC
//	PERCENTRANK
//	PERCENTRANK.EXC
//	PERCENTRANK.INC
//	PERMUT
//	PERMUTATIONA
//	PHI
//	PI
//	PMT
//	POISSON
//	POISSON.DIST
//	POWER
//	PPMT
//	PRICE
//	PRICEDISC
//	PRICEMAT
//	PROB
//	PRODUCT
//	PROPER
//	PV
//	QUARTILE
//	QUARTILE.EXC
//	QUARTILE.INC
//	QUOTIENT
//	RADIANS
//	RAND
//	RANDBETWEEN
//	RANK
//	RANK.EQ
//	RATE
//	RECEIVED
//	REPLACE
//	REPLACEB
//	REPT
//	RIGHT
//	RIGHTB
//	ROMAN
//	ROUND
//	ROUNDDOWN
//	ROUNDUP
//	ROW
//	ROWS
//	RRI
//	RSQ
//	SEARCH
//	SEARCHB
//	SEC
//	SECH
//	SECOND
//	SERIESSUM
//	SHEET
//	SHEETS
//	SIGN
//	SIN
//	SINH
//	SKEW
//	SKEW.P
//	SLN
//	SLOPE
//	SMALL
//	SQRT
//	SQRTPI
//	STANDARDIZE
//	STDEV
//	STDEV.P
//	STDEV.S
//	STDEVA
//	STDEVP
//	STDEVPA
//	STEYX
//	SUBSTITUTE
//	SUBTOTAL
//	SUM
//	SUMIF
//	SUMIFS
//	SUMPRODUCT
//	SUMSQ
//	SUMX2MY2
//	SUMX2PY2
//	SUMXMY2
//	SWITCH
//	SYD
//	T
//	T.DIST
//	T.DIST.2T
//	T.DIST.RT
//	T.INV
//	T.INV.2T
//	T.TEST
//	TAN
//	TANH
//	TBILLEQ
//	TBILLPRICE
//	TBILLYIELD
//	TDIST
//	TEXT
//	TEXTAFTER
//	TEXTBEFORE
//	TEXTJOIN
//	TIME
//	TIMEVALUE
//	TINV
//	TODAY
//	TRANSPOSE
//	TREND
//	TRIM
//	TRIMMEAN
//	TRUE
//	TRUNC
//	TTEST
//	TYPE
//	UNICHAR
//	UNICODE
//	UPPER
//	VALUE
//	VALUETOTEXT
//	VAR
//	VAR.P
//	VAR.S
//	VARA
//	VARP
//	VARPA
//	VDB
//	VLOOKUP
//	WEEKDAY
//	WEEKNUM
//	WEIBULL
//	WEIBULL.DIST
//	WORKDAY
//	WORKDAY.INTL
//	XIRR
//	XLOOKUP
//	XNPV
//	XOR
//	YEAR
//	YEARFRAC
//	YIELD
//	YIELDDISC
//	YIELDMAT
//	Z.TEST
//	ZTEST
func (f *File) CalcCellValue(sheet, cell string, opts ...Options) (result string, err error) {
	var (
		rawCellValue = getOptions(opts...).RawCellValue
		styleIdx     int
		token        formulaArg
	)
	if token, err = f.calcCellValue(&calcContext{
		entry:             fmt.Sprintf("%s!%s", sheet, cell),
		maxCalcIterations: getOptions(opts...).MaxCalcIterations,
		iterations:        make(map[string]uint),
		iterationsCache:   make(map[string]formulaArg),
	}, sheet, cell); err != nil {
		result = token.String
		return
	}
	if !rawCellValue {
		styleIdx, _ = f.GetCellStyle(sheet, cell)
	}
	result = token.Value()
	if isNum, precision, decimal := isNumeric(result); isNum {
		if precision > 15 {
			result, err = f.formattedValue(&xlsxC{S: styleIdx, V: strings.ToUpper(strconv.FormatFloat(decimal, 'G', 15, 64))}, rawCellValue, CellTypeNumber)
			return
		}
		if !strings.HasPrefix(result, "0") {
			result, err = f.formattedValue(&xlsxC{S: styleIdx, V: strings.ToUpper(strconv.FormatFloat(decimal, 'f', -1, 64))}, rawCellValue, CellTypeNumber)
		}
	}
	return
}

// calcCellValue calculate cell value by given context, worksheet name and cell
// reference.
func (f *File) calcCellValue(ctx *calcContext, sheet, cell string) (result formulaArg, err error) {
	var formula string
	if formula, err = f.GetCellFormula(sheet, cell); err != nil {
		return
	}
	ps := efp.ExcelParser()
	tokens := ps.Parse(formula)
	if tokens == nil {
		return
	}
	result, err = f.evalInfixExp(ctx, sheet, cell, tokens)
	return
}

// getPriority calculate arithmetic operator priority.
func getPriority(token efp.Token) (pri int) {
	pri = tokenPriority[token.TValue]
	if token.TValue == "-" && token.TType == efp.TokenTypeOperatorPrefix {
		pri = 6
	}
	if isBeginParenthesesToken(token) { // (
		pri = 0
	}
	return
}

// newNumberFormulaArg constructs a number formula argument.
func newNumberFormulaArg(n float64) formulaArg {
	if math.IsNaN(n) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return formulaArg{Type: ArgNumber, Number: n}
}

// newStringFormulaArg constructs a string formula argument.
func newStringFormulaArg(s string) formulaArg {
	return formulaArg{Type: ArgString, String: s}
}

// newMatrixFormulaArg constructs a matrix formula argument.
func newMatrixFormulaArg(m [][]formulaArg) formulaArg {
	return formulaArg{Type: ArgMatrix, Matrix: m}
}

// newListFormulaArg create a list formula argument.
func newListFormulaArg(l []formulaArg) formulaArg {
	return formulaArg{Type: ArgList, List: l}
}

// newBoolFormulaArg constructs a boolean formula argument.
func newBoolFormulaArg(b bool) formulaArg {
	var n float64
	if b {
		n = 1
	}
	return formulaArg{Type: ArgNumber, Number: n, Boolean: true}
}

// newErrorFormulaArg create an error formula argument of a given type with a
// specified error message.
func newErrorFormulaArg(formulaError, msg string) formulaArg {
	return formulaArg{Type: ArgError, String: formulaError, Error: msg}
}

// newEmptyFormulaArg create an empty formula argument.
func newEmptyFormulaArg() formulaArg {
	return formulaArg{Type: ArgEmpty}
}

// evalInfixExp evaluate syntax analysis by given infix expression after
// lexical analysis. Evaluate an infix expression containing formulas by
// stacks:
//
//	opd  - Operand
//	opt  - Operator
//	opf  - Operation formula
//	opfd - Operand of the operation formula
//	opft - Operator of the operation formula
//	args - Arguments list of the operation formula
//
// TODO: handle subtypes: Nothing, Text, Logical, Error, Concatenation, Intersection, Union
func (f *File) evalInfixExp(ctx *calcContext, sheet, cell string, tokens []efp.Token) (formulaArg, error) {
	var err error
	opdStack, optStack, opfStack, opfdStack, opftStack, argsStack := NewStack(), NewStack(), NewStack(), NewStack(), NewStack(), NewStack()
	var inArray, inArrayRow bool
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		// out of function stack
		if opfStack.Len() == 0 {
			if err = f.parseToken(ctx, sheet, token, opdStack, optStack); err != nil {
				return newEmptyFormulaArg(), err
			}
		}

		// function start
		if isFunctionStartToken(token) {
			if token.TValue == "ARRAY" {
				inArray = true
				continue
			}
			if token.TValue == "ARRAYROW" {
				inArrayRow = true
				continue
			}
			opfStack.Push(token)
			argsStack.Push(list.New().Init())
			opftStack.Push(token) // to know which operators belong to a function use the function as a separator
			continue
		}

		// in function stack, walk 2 token at once
		if opfStack.Len() > 0 {
			var nextToken efp.Token
			if i+1 < len(tokens) {
				nextToken = tokens[i+1]
			}

			// current token is args or range, skip next token, order required: parse reference first
			if token.TSubType == efp.TokenSubTypeRange {
				if opftStack.Peek().(efp.Token) != opfStack.Peek().(efp.Token) {
					refTo := f.getDefinedNameRefTo(token.TValue, sheet)
					if refTo != "" {
						token.TValue = refTo
					}
					// parse reference: must reference at here
					result, err := f.parseReference(ctx, sheet, token.TValue)
					if err != nil {
						return result, err
					}
					opfdStack.Push(result)
					continue
				}
				if nextToken.TType == efp.TokenTypeArgument || nextToken.TType == efp.TokenTypeFunction {
					// parse reference: reference or range at here
					refTo := f.getDefinedNameRefTo(token.TValue, sheet)
					if refTo != "" {
						token.TValue = refTo
					}
					result, err := f.parseReference(ctx, sheet, token.TValue)
					if err != nil {
						return result, err
					}
					// when current token is range, next token is argument and opfdStack not empty,
					// should push value to opfdStack and continue
					if nextToken.TType == efp.TokenTypeArgument && !opfdStack.Empty() {
						opfdStack.Push(result)
						continue
					}
					argsStack.Peek().(*list.List).PushBack(result)
					continue
				}
			}

			if isEndParenthesesToken(token) && isBeginParenthesesToken(opftStack.Peek().(efp.Token)) {
				if arg := argsStack.Peek().(*list.List).Back(); arg != nil {
					opfdStack.Push(arg.Value.(formulaArg))
					argsStack.Peek().(*list.List).Remove(arg)
				}
			}

			// check current token is opft
			if err = f.parseToken(ctx, sheet, token, opfdStack, opftStack); err != nil {
				return newEmptyFormulaArg(), err
			}

			// current token is arg
			if token.TType == efp.TokenTypeArgument {
				for opftStack.Peek().(efp.Token) != opfStack.Peek().(efp.Token) {
					// calculate trigger
					topOpt := opftStack.Peek().(efp.Token)
					if err := calculate(opfdStack, topOpt); err != nil {
						argsStack.Peek().(*list.List).PushFront(newErrorFormulaArg(formulaErrorVALUE, err.Error()))
					}
					opftStack.Pop()
				}
				if !opfdStack.Empty() {
					argsStack.Peek().(*list.List).PushBack(opfdStack.Pop().(formulaArg))
				}
				continue
			}

			if inArrayRow && isOperand(token) {
				continue
			}
			if inArrayRow && isFunctionStopToken(token) {
				inArrayRow = false
				continue
			}
			if inArray && isFunctionStopToken(token) {
				argsStack.Peek().(*list.List).PushBack(opfdStack.Pop())
				inArray = false
				continue
			}
			if errArg := f.evalInfixExpFunc(ctx, sheet, cell, token, nextToken, opfStack, opdStack, opftStack, opfdStack, argsStack); errArg.Type == ArgError {
				return errArg, errors.New(errArg.Error)
			}
		}
	}
	for optStack.Len() != 0 {
		topOpt := optStack.Peek().(efp.Token)
		if err = calculate(opdStack, topOpt); err != nil {
			return newEmptyFormulaArg(), err
		}
		optStack.Pop()
	}
	if opdStack.Len() == 0 {
		return newEmptyFormulaArg(), ErrInvalidFormula
	}
	return opdStack.Peek().(formulaArg), err
}

// evalInfixExpFunc evaluate formula function in the infix expression.
func (f *File) evalInfixExpFunc(ctx *calcContext, sheet, cell string, token, nextToken efp.Token, opfStack, opdStack, opftStack, opfdStack, argsStack *Stack) formulaArg {
	if !isFunctionStopToken(token) {
		return newEmptyFormulaArg()
	}
	prepareEvalInfixExp(opfStack, opftStack, opfdStack, argsStack)
	// call formula function to evaluate
	arg := callFuncByName(&formulaFuncs{f: f, sheet: sheet, cell: cell, ctx: ctx}, strings.NewReplacer(
		"_xlfn.", "", ".", "dot").Replace(opfStack.Peek().(efp.Token).TValue),
		[]reflect.Value{reflect.ValueOf(argsStack.Peek().(*list.List))})
	if arg.Type == ArgError && opfStack.Len() == 1 {
		return arg
	}
	argsStack.Pop()
	opftStack.Pop() // remove current function separator
	opfStack.Pop()
	if opfStack.Len() > 0 { // still in function stack
		if nextToken.TType == efp.TokenTypeOperatorInfix || (opftStack.Len() > 1 && opfdStack.Len() > 0) {
			// mathematics calculate in formula function
			opfdStack.Push(arg)
			return newEmptyFormulaArg()
		}
		argsStack.Peek().(*list.List).PushBack(arg)
		return newEmptyFormulaArg()
	}
	if arg.Type == ArgMatrix && len(arg.Matrix) > 0 && len(arg.Matrix[0]) > 0 {
		opdStack.Push(arg.Matrix[0][0])
		return newEmptyFormulaArg()
	}
	opdStack.Push(arg)
	return newEmptyFormulaArg()
}

// prepareEvalInfixExp check the token and stack state for formula function
// evaluate.
func prepareEvalInfixExp(opfStack, opftStack, opfdStack, argsStack *Stack) {
	// current token is function stop
	for opftStack.Peek().(efp.Token) != opfStack.Peek().(efp.Token) {
		// calculate trigger
		topOpt := opftStack.Peek().(efp.Token)
		if err := calculate(opfdStack, topOpt); err != nil {
			argsStack.Peek().(*list.List).PushBack(newErrorFormulaArg(err.Error(), err.Error()))
			opftStack.Pop()
			continue
		}
		opftStack.Pop()
	}
	argument := true
	if opftStack.Len() > 2 && opfdStack.Len() == 1 {
		topOpt := opftStack.Pop()
		if opftStack.Peek().(efp.Token).TType == efp.TokenTypeOperatorInfix {
			argument = false
		}
		opftStack.Push(topOpt)
	}
	// push opfd to args
	if argument && opfdStack.Len() > 0 {
		argsStack.Peek().(*list.List).PushBack(opfdStack.Pop().(formulaArg))
	}
}

// calcPow evaluate exponentiation arithmetic operations.
func calcPow(rOpd, lOpd formulaArg, opdStack *Stack) error {
	lOpdVal := lOpd.ToNumber()
	if lOpdVal.Type != ArgNumber {
		return errors.New(lOpdVal.Value())
	}
	rOpdVal := rOpd.ToNumber()
	if rOpdVal.Type != ArgNumber {
		return errors.New(rOpdVal.Value())
	}
	opdStack.Push(newNumberFormulaArg(math.Pow(lOpdVal.Number, rOpdVal.Number)))
	return nil
}

// calcEq evaluate equal arithmetic operations.
func calcEq(rOpd, lOpd formulaArg, opdStack *Stack) error {
	opdStack.Push(newBoolFormulaArg(rOpd.Value() == lOpd.Value()))
	return nil
}

// calcNEq evaluate not equal arithmetic operations.
func calcNEq(rOpd, lOpd formulaArg, opdStack *Stack) error {
	opdStack.Push(newBoolFormulaArg(rOpd.Value() != lOpd.Value()))
	return nil
}

// calcL evaluate less than arithmetic operations.
func calcL(rOpd, lOpd formulaArg, opdStack *Stack) error {
	if rOpd.Type == ArgNumber && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(lOpd.Number < rOpd.Number))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(strings.Compare(lOpd.Value(), rOpd.Value()) == -1))
	}
	if rOpd.Type == ArgNumber && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(false))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(true))
	}
	return nil
}

// calcLe evaluate less than or equal arithmetic operations.
func calcLe(rOpd, lOpd formulaArg, opdStack *Stack) error {
	if rOpd.Type == ArgNumber && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(lOpd.Number <= rOpd.Number))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(strings.Compare(lOpd.Value(), rOpd.Value()) != 1))
	}
	if rOpd.Type == ArgNumber && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(false))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(true))
	}
	return nil
}

// calcG evaluate greater than arithmetic operations.
func calcG(rOpd, lOpd formulaArg, opdStack *Stack) error {
	if rOpd.Type == ArgNumber && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(lOpd.Number > rOpd.Number))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(strings.Compare(lOpd.Value(), rOpd.Value()) == 1))
	}
	if rOpd.Type == ArgNumber && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(true))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(false))
	}
	return nil
}

// calcGe evaluate greater than or equal arithmetic operations.
func calcGe(rOpd, lOpd formulaArg, opdStack *Stack) error {
	if rOpd.Type == ArgNumber && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(lOpd.Number >= rOpd.Number))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(strings.Compare(lOpd.Value(), rOpd.Value()) != -1))
	}
	if rOpd.Type == ArgNumber && lOpd.Type == ArgString {
		opdStack.Push(newBoolFormulaArg(true))
	}
	if rOpd.Type == ArgString && lOpd.Type == ArgNumber {
		opdStack.Push(newBoolFormulaArg(false))
	}
	return nil
}

// calcSplice evaluate splice '&' operations.
func calcSplice(rOpd, lOpd formulaArg, opdStack *Stack) error {
	opdStack.Push(newStringFormulaArg(lOpd.Value() + rOpd.Value()))
	return nil
}

// calcAdd evaluate addition arithmetic operations.
func calcAdd(rOpd, lOpd formulaArg, opdStack *Stack) error {
	lOpdVal := lOpd.ToNumber()
	if lOpdVal.Type != ArgNumber {
		return errors.New(lOpdVal.Value())
	}
	rOpdVal := rOpd.ToNumber()
	if rOpdVal.Type != ArgNumber {
		return errors.New(rOpdVal.Value())
	}
	opdStack.Push(newNumberFormulaArg(lOpdVal.Number + rOpdVal.Number))
	return nil
}

// calcSubtract evaluate subtraction arithmetic operations.
func calcSubtract(rOpd, lOpd formulaArg, opdStack *Stack) error {
	lOpdVal := lOpd.ToNumber()
	if lOpdVal.Type != ArgNumber {
		return errors.New(lOpdVal.Value())
	}
	rOpdVal := rOpd.ToNumber()
	if rOpdVal.Type != ArgNumber {
		return errors.New(rOpdVal.Value())
	}
	opdStack.Push(newNumberFormulaArg(lOpdVal.Number - rOpdVal.Number))
	return nil
}

// calcMultiply evaluate multiplication arithmetic operations.
func calcMultiply(rOpd, lOpd formulaArg, opdStack *Stack) error {
	lOpdVal := lOpd.ToNumber()
	if lOpdVal.Type != ArgNumber {
		return errors.New(lOpdVal.Value())
	}
	rOpdVal := rOpd.ToNumber()
	if rOpdVal.Type != ArgNumber {
		return errors.New(rOpdVal.Value())
	}
	opdStack.Push(newNumberFormulaArg(lOpdVal.Number * rOpdVal.Number))
	return nil
}

// calcDiv evaluate division arithmetic operations.
func calcDiv(rOpd, lOpd formulaArg, opdStack *Stack) error {
	lOpdVal := lOpd.ToNumber()
	if lOpdVal.Type != ArgNumber {
		return errors.New(lOpdVal.Value())
	}
	rOpdVal := rOpd.ToNumber()
	if rOpdVal.Type != ArgNumber {
		return errors.New(rOpdVal.Value())
	}
	if rOpdVal.Number == 0 {
		return errors.New(formulaErrorDIV)
	}
	opdStack.Push(newNumberFormulaArg(lOpdVal.Number / rOpdVal.Number))
	return nil
}

// calculate evaluate basic arithmetic operations.
func calculate(opdStack *Stack, opt efp.Token) error {
	if opt.TValue == "-" && opt.TType == efp.TokenTypeOperatorPrefix {
		if opdStack.Len() < 1 {
			return ErrInvalidFormula
		}
		opd := opdStack.Pop().(formulaArg)
		opdStack.Push(newNumberFormulaArg(0 - opd.ToNumber().Number))
	}
	if opt.TValue == "-" && opt.TType == efp.TokenTypeOperatorInfix {
		if opdStack.Len() < 2 {
			return ErrInvalidFormula
		}
		rOpd := opdStack.Pop().(formulaArg)
		lOpd := opdStack.Pop().(formulaArg)
		if err := calcSubtract(rOpd, lOpd, opdStack); err != nil {
			return err
		}
	}
	tokenCalcFunc := map[string]func(rOpd, lOpd formulaArg, opdStack *Stack) error{
		"^":  calcPow,
		"*":  calcMultiply,
		"/":  calcDiv,
		"+":  calcAdd,
		"=":  calcEq,
		"<>": calcNEq,
		"<":  calcL,
		"<=": calcLe,
		">":  calcG,
		">=": calcGe,
		"&":  calcSplice,
	}
	fn, ok := tokenCalcFunc[opt.TValue]
	if ok {
		if opdStack.Len() < 2 {
			return ErrInvalidFormula
		}
		rOpd := opdStack.Pop().(formulaArg)
		lOpd := opdStack.Pop().(formulaArg)
		if rOpd.Type == ArgError {
			return errors.New(rOpd.Value())
		}
		if lOpd.Type == ArgError {
			return errors.New(lOpd.Value())
		}
		if err := fn(rOpd, lOpd, opdStack); err != nil {
			return err
		}
	}
	return nil
}

// parseOperatorPrefixToken parse operator prefix token.
func (f *File) parseOperatorPrefixToken(optStack, opdStack *Stack, token efp.Token) (err error) {
	if optStack.Len() == 0 {
		optStack.Push(token)
		return
	}
	tokenPriority := getPriority(token)
	topOpt := optStack.Peek().(efp.Token)
	topOptPriority := getPriority(topOpt)
	if tokenPriority > topOptPriority {
		optStack.Push(token)
		return
	}
	for tokenPriority <= topOptPriority {
		optStack.Pop()
		if err = calculate(opdStack, topOpt); err != nil {
			return
		}
		if optStack.Len() > 0 {
			topOpt = optStack.Peek().(efp.Token)
			topOptPriority = getPriority(topOpt)
			continue
		}
		break
	}
	optStack.Push(token)
	return
}

// isFunctionStartToken determine if the token is function start.
func isFunctionStartToken(token efp.Token) bool {
	return token.TType == efp.TokenTypeFunction && token.TSubType == efp.TokenSubTypeStart
}

// isFunctionStopToken determine if the token is function stop.
func isFunctionStopToken(token efp.Token) bool {
	return token.TType == efp.TokenTypeFunction && token.TSubType == efp.TokenSubTypeStop
}

// isBeginParenthesesToken determine if the token is begin parentheses: (.
func isBeginParenthesesToken(token efp.Token) bool {
	return token.TType == efp.TokenTypeSubexpression && token.TSubType == efp.TokenSubTypeStart
}

// isEndParenthesesToken determine if the token is end parentheses: ).
func isEndParenthesesToken(token efp.Token) bool {
	return token.TType == efp.TokenTypeSubexpression && token.TSubType == efp.TokenSubTypeStop
}

// isOperatorPrefixToken determine if the token is parse operator prefix
// token.
func isOperatorPrefixToken(token efp.Token) bool {
	_, ok := tokenPriority[token.TValue]
	return (token.TValue == "-" && token.TType == efp.TokenTypeOperatorPrefix) || (ok && token.TType == efp.TokenTypeOperatorInfix)
}

// isOperand determine if the token is parse operand.
func isOperand(token efp.Token) bool {
	return token.TType == efp.TokenTypeOperand && (token.TSubType == efp.TokenSubTypeNumber || token.TSubType == efp.TokenSubTypeText || token.TSubType == efp.TokenSubTypeLogical)
}

// tokenToFormulaArg create a formula argument by given token.
func tokenToFormulaArg(token efp.Token) formulaArg {
	switch token.TSubType {
	case efp.TokenSubTypeLogical:
		return newBoolFormulaArg(strings.EqualFold(token.TValue, "TRUE"))
	case efp.TokenSubTypeNumber:
		num, _ := strconv.ParseFloat(token.TValue, 64)
		return newNumberFormulaArg(num)
	default:
		return newStringFormulaArg(token.TValue)
	}
}

// formulaArgToToken create a token by given formula argument.
func formulaArgToToken(arg formulaArg) efp.Token {
	switch arg.Type {
	case ArgNumber:
		if arg.Boolean {
			return efp.Token{TValue: arg.Value(), TType: efp.TokenTypeOperand, TSubType: efp.TokenSubTypeLogical}
		}
		return efp.Token{TValue: arg.Value(), TType: efp.TokenTypeOperand, TSubType: efp.TokenSubTypeNumber}
	default:
		return efp.Token{TValue: arg.Value(), TType: efp.TokenTypeOperand, TSubType: efp.TokenSubTypeText}
	}
}

// parseToken parse basic arithmetic operator priority and evaluate based on
// operators and operands.
func (f *File) parseToken(ctx *calcContext, sheet string, token efp.Token, opdStack, optStack *Stack) error {
	// parse reference: must reference at here
	if token.TSubType == efp.TokenSubTypeRange {
		refTo := f.getDefinedNameRefTo(token.TValue, sheet)
		if refTo != "" {
			token.TValue = refTo
		}
		result, err := f.parseReference(ctx, sheet, token.TValue)
		if err != nil {
			return errors.New(formulaErrorNAME)
		}
		token = formulaArgToToken(result)
	}
	if isOperatorPrefixToken(token) {
		if err := f.parseOperatorPrefixToken(optStack, opdStack, token); err != nil {
			return err
		}
	}
	if isBeginParenthesesToken(token) { // (
		optStack.Push(token)
	}
	if isEndParenthesesToken(token) { // )
		for !isBeginParenthesesToken(optStack.Peek().(efp.Token)) { // != (
			topOpt := optStack.Peek().(efp.Token)
			if err := calculate(opdStack, topOpt); err != nil {
				return err
			}
			optStack.Pop()
		}
		optStack.Pop()
	}
	if token.TType == efp.TokenTypeOperatorPostfix && !opdStack.Empty() {
		topOpd := opdStack.Pop().(formulaArg)
		opdStack.Push(newNumberFormulaArg(topOpd.Number / 100))
	}
	// opd
	if isOperand(token) {
		opdStack.Push(tokenToFormulaArg(token))
	}
	return nil
}

// parseRef parse reference for a cell, column name or row number.
func (f *File) parseRef(ref string) (cellRef, bool, bool, error) {
	var (
		err, colErr, rowErr error
		cr                  cellRef
		cell                = ref
		tokens              = strings.Split(ref, "!")
	)
	if len(tokens) == 2 { // have a worksheet
		cr.Sheet, cell = tokens[0], tokens[1]
	}
	if cr.Col, cr.Row, err = CellNameToCoordinates(cell); err != nil {
		if cr.Col, colErr = ColumnNameToNumber(cell); colErr == nil { // cast to column
			return cr, true, false, nil
		}
		if cr.Row, rowErr = strconv.Atoi(cell); rowErr == nil { // cast to row
			return cr, false, true, nil
		}
		return cr, false, false, err
	}
	return cr, false, false, err
}

// prepareCellRange checking and convert cell reference to a cell range.
func (cr *cellRange) prepareCellRange(col, row bool, cellRef cellRef) error {
	if col {
		cellRef.Row = TotalRows
	}
	if row {
		cellRef.Col = MaxColumns
	}
	if cellRef.Sheet == "" {
		cellRef.Sheet = cr.From.Sheet
	}
	if cr.From.Sheet != cellRef.Sheet || cr.To.Sheet != cellRef.Sheet {
		return errors.New("invalid reference")
	}
	if cr.From.Col > cellRef.Col {
		cr.From.Col = cellRef.Col
	}
	if cr.From.Row > cellRef.Row {
		cr.From.Row = cellRef.Row
	}
	if cr.To.Col < cellRef.Col {
		cr.To.Col = cellRef.Col
	}
	if cr.To.Row < cellRef.Row {
		cr.To.Row = cellRef.Row
	}
	return nil
}

// parseReference parse reference and extract values by given reference
// characters and default sheet name.
func (f *File) parseReference(ctx *calcContext, sheet, reference string) (formulaArg, error) {
	reference = strings.ReplaceAll(reference, "$", "")
	ranges, cellRanges, cellRefs := strings.Split(reference, ":"), list.New(), list.New()
	if len(ranges) > 1 {
		var cr cellRange
		for i, ref := range ranges {
			cellRef, col, row, err := f.parseRef(ref)
			if err != nil {
				return newErrorFormulaArg(formulaErrorNAME, "invalid reference"), errors.New("invalid reference")
			}
			if i == 0 {
				if col {
					cellRef.Row = 1
				}
				if row {
					cellRef.Col = 1
				}
				if cellRef.Sheet == "" {
					cellRef.Sheet = sheet
				}
				cr.From, cr.To = cellRef, cellRef
				continue
			}
			if err := cr.prepareCellRange(col, row, cellRef); err != nil {
				return newErrorFormulaArg(formulaErrorNAME, err.Error()), err
			}
		}
		cellRanges.PushBack(cr)
		return f.rangeResolver(ctx, cellRefs, cellRanges)
	}
	cellRef, _, _, err := f.parseRef(reference)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNAME, "invalid reference"), errors.New("invalid reference")
	}
	if cellRef.Sheet == "" {
		cellRef.Sheet = sheet
	}
	cellRefs.PushBack(cellRef)
	return f.rangeResolver(ctx, cellRefs, cellRanges)
}

// prepareValueRange prepare value range.
func prepareValueRange(cr cellRange, valueRange []int) {
	if cr.From.Row < valueRange[0] || valueRange[0] == 0 {
		valueRange[0] = cr.From.Row
	}
	if cr.From.Col < valueRange[2] || valueRange[2] == 0 {
		valueRange[2] = cr.From.Col
	}
	if cr.To.Row > valueRange[1] || valueRange[1] == 0 {
		valueRange[1] = cr.To.Row
	}
	if cr.To.Col > valueRange[3] || valueRange[3] == 0 {
		valueRange[3] = cr.To.Col
	}
}

// prepareValueRef prepare value reference.
func prepareValueRef(cr cellRef, valueRange []int) {
	if cr.Row < valueRange[0] || valueRange[0] == 0 {
		valueRange[0] = cr.Row
	}
	if cr.Col < valueRange[2] || valueRange[2] == 0 {
		valueRange[2] = cr.Col
	}
	if cr.Row > valueRange[1] || valueRange[1] == 0 {
		valueRange[1] = cr.Row
	}
	if cr.Col > valueRange[3] || valueRange[3] == 0 {
		valueRange[3] = cr.Col
	}
}

// cellResolver calc cell value by given worksheet name, cell reference and context.
func (f *File) cellResolver(ctx *calcContext, sheet, cell string) (formulaArg, error) {
	var (
		arg   formulaArg
		value string
		err   error
	)
	ref := fmt.Sprintf("%s!%s", sheet, cell)
	if formula, _ := f.GetCellFormula(sheet, cell); len(formula) != 0 {
		ctx.mu.Lock()
		if ctx.entry != ref {
			if ctx.iterations[ref] <= f.options.MaxCalcIterations {
				ctx.iterations[ref]++
				ctx.mu.Unlock()
				arg, _ = f.calcCellValue(ctx, sheet, cell)
				ctx.iterationsCache[ref] = arg
				return arg, nil
			}
			ctx.mu.Unlock()
			return ctx.iterationsCache[ref], nil
		}
		ctx.mu.Unlock()
	}
	if value, err = f.GetCellValue(sheet, cell, Options{RawCellValue: true}); err != nil {
		return arg, err
	}
	arg = newStringFormulaArg(value)
	cellType, _ := f.GetCellType(sheet, cell)
	switch cellType {
	case CellTypeBool:
		return arg.ToBool(), err
	case CellTypeNumber, CellTypeUnset:
		if arg.Value() == "" {
			return newEmptyFormulaArg(), err
		}
		return arg.ToNumber(), err
	case CellTypeInlineString, CellTypeSharedString:
		return arg, err
	default:
		return newEmptyFormulaArg(), err
	}
}

// rangeResolver extract value as string from given reference and range list.
// This function will not ignore the empty cell. For example, A1:A2:A2:B3 will
// be reference A1:B3.
func (f *File) rangeResolver(ctx *calcContext, cellRefs, cellRanges *list.List) (arg formulaArg, err error) {
	arg.cellRefs, arg.cellRanges = cellRefs, cellRanges
	// value range order: from row, to row, from column, to column
	valueRange := []int{0, 0, 0, 0}
	var sheet string
	// prepare value range
	for temp := cellRanges.Front(); temp != nil; temp = temp.Next() {
		cr := temp.Value.(cellRange)
		rng := []int{cr.From.Col, cr.From.Row, cr.To.Col, cr.To.Row}
		_ = sortCoordinates(rng)
		cr.From.Col, cr.From.Row, cr.To.Col, cr.To.Row = rng[0], rng[1], rng[2], rng[3]
		prepareValueRange(cr, valueRange)
		if cr.From.Sheet != "" {
			sheet = cr.From.Sheet
		}
	}
	for temp := cellRefs.Front(); temp != nil; temp = temp.Next() {
		cr := temp.Value.(cellRef)
		if cr.Sheet != "" {
			sheet = cr.Sheet
		}
		prepareValueRef(cr, valueRange)
	}
	// extract value from ranges
	if cellRanges.Len() > 0 {
		arg.Type = ArgMatrix
		for row := valueRange[0]; row <= valueRange[1]; row++ {
			var matrixRow []formulaArg
			for col := valueRange[2]; col <= valueRange[3]; col++ {
				var cell string
				var value formulaArg
				if cell, err = CoordinatesToCellName(col, row); err != nil {
					return
				}
				if value, err = f.cellResolver(ctx, sheet, cell); err != nil {
					return
				}
				matrixRow = append(matrixRow, value)
			}
			arg.Matrix = append(arg.Matrix, matrixRow)
		}
		return
	}
	// extract value from references
	for temp := cellRefs.Front(); temp != nil; temp = temp.Next() {
		cr := temp.Value.(cellRef)
		var cell string
		if cell, err = CoordinatesToCellName(cr.Col, cr.Row); err != nil {
			return
		}
		if arg, err = f.cellResolver(ctx, cr.Sheet, cell); err != nil {
			return
		}
		arg.cellRefs, arg.cellRanges = cellRefs, cellRanges
	}
	return
}

// callFuncByName calls the no error or only error return function with
// reflect by given receiver, name and parameters.
func callFuncByName(receiver interface{}, name string, params []reflect.Value) (arg formulaArg) {
	function := reflect.ValueOf(receiver).MethodByName(name)
	if function.IsValid() {
		rt := function.Call(params)
		if len(rt) == 0 {
			return
		}
		arg = rt[0].Interface().(formulaArg)
		return
	}
	return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("not support %s function", name))
}

// formulaCriteriaParser parse formula criteria.
func formulaCriteriaParser(exp formulaArg) *formulaCriteria {
	prepareValue := func(cond string) (expected float64, err error) {
		percentile := 1.0
		if strings.HasSuffix(cond, "%") {
			cond = strings.TrimSuffix(cond, "%")
			percentile /= 100
		}
		if expected, err = strconv.ParseFloat(cond, 64); err != nil {
			return
		}
		expected *= percentile
		return
	}
	fc, val := &formulaCriteria{}, exp.Value()
	if val == "" {
		return fc
	}
	for i, re := range formulaFormats {
		if match := re.FindStringSubmatch(val); len(match) > 1 {
			fc.Condition = newStringFormulaArg(match[1])
			if num, err := prepareValue(match[1]); err == nil {
				fc.Condition = newNumberFormulaArg(num)
			}
			fc.Type = formulaCriterias[i]
			return fc
		}
	}
	if strings.Contains(val, "?") {
		val = strings.ReplaceAll(val, "?", ".")
	}
	if strings.Contains(val, "*") {
		val = strings.ReplaceAll(val, "*", ".*")
	}
	fc.Type, fc.Condition = criteriaRegexp, newStringFormulaArg(val)
	if num := fc.Condition.ToNumber(); num.Type == ArgNumber {
		fc.Condition = num
	}
	return fc
}

// formulaCriteriaEval evaluate formula criteria expression.
func formulaCriteriaEval(val formulaArg, criteria *formulaCriteria) (result bool, err error) {
	s := NewStack()
	tokenCalcFunc := map[byte]func(rOpd, lOpd formulaArg, opdStack *Stack) error{
		criteriaEq: calcEq,
		criteriaNe: calcNEq,
		criteriaL:  calcL,
		criteriaLe: calcLe,
		criteriaG:  calcG,
		criteriaGe: calcGe,
	}
	switch criteria.Type {
	case criteriaEq, criteriaLe, criteriaGe, criteriaNe, criteriaL, criteriaG:
		if fn, ok := tokenCalcFunc[criteria.Type]; ok {
			if _ = fn(criteria.Condition, val, s); s.Len() > 0 {
				return s.Pop().(formulaArg).Number == 1, err
			}
		}
	case criteriaRegexp:
		return regexp.MatchString(criteria.Condition.Value(), val.Value())
	}
	return
}

// Engineering Functions

// BESSELI function the modified Bessel function, which is equivalent to the
// Bessel function evaluated for purely imaginary arguments. The syntax of
// the Besseli function is:
//
//	BESSELI(x,n)
func (fn *formulaFuncs) BESSELI(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BESSELI requires 2 numeric arguments")
	}
	return fn.bassel(argsList, true)
}

// BESSELJ function returns the Bessel function, Jn(x), for a specified order
// and value of x. The syntax of the function is:
//
//	BESSELJ(x,n)
func (fn *formulaFuncs) BESSELJ(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BESSELJ requires 2 numeric arguments")
	}
	return fn.bassel(argsList, false)
}

// bassel is an implementation of the formula functions BESSELI and BESSELJ.
func (fn *formulaFuncs) bassel(argsList *list.List, modfied bool) formulaArg {
	x, n := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Back().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	if n.Type != ArgNumber {
		return n
	}
	max, x1 := 100, x.Number*0.5
	x2 := x1 * x1
	x1 = math.Pow(x1, n.Number)
	n1, n2, n3, n4, add := fact(n.Number), 1.0, 0.0, n.Number, false
	result := x1 / n1
	t := result * 0.9
	for result != t && max != 0 {
		x1 *= x2
		n3++
		n1 *= n3
		n4++
		n2 *= n4
		t = result
		r := x1 / n1 / n2
		if modfied || add {
			result += r
		} else {
			result -= r
		}
		max--
		add = !add
	}
	return newNumberFormulaArg(result)
}

// BESSELK function calculates the modified Bessel functions, Kn(x), which are
// also known as the hyperbolic Bessel Functions. These are the equivalent of
// the Bessel functions, evaluated for purely imaginary arguments. The syntax
// of the function is:
//
//	BESSELK(x,n)
func (fn *formulaFuncs) BESSELK(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BESSELK requires 2 numeric arguments")
	}
	x, n := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Back().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	if n.Type != ArgNumber {
		return n
	}
	if x.Number <= 0 || n.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	var result float64
	switch math.Floor(n.Number) {
	case 0:
		result = fn.besselK0(x)
	case 1:
		result = fn.besselK1(x)
	default:
		result = fn.besselK2(x, n)
	}
	return newNumberFormulaArg(result)
}

// besselK0 is an implementation of the formula function BESSELK.
func (fn *formulaFuncs) besselK0(x formulaArg) float64 {
	var y float64
	if x.Number <= 2 {
		n2 := x.Number * 0.5
		y = n2 * n2
		args := list.New()
		args.PushBack(x)
		args.PushBack(newNumberFormulaArg(0))
		return -math.Log(n2)*fn.BESSELI(args).Number +
			(-0.57721566 + y*(0.42278420+y*(0.23069756+y*(0.3488590e-1+y*(0.262698e-2+y*
				(0.10750e-3+y*0.74e-5))))))
	}
	y = 2 / x.Number
	return math.Exp(-x.Number) / math.Sqrt(x.Number) *
		(1.25331414 + y*(-0.7832358e-1+y*(0.2189568e-1+y*(-0.1062446e-1+y*
			(0.587872e-2+y*(-0.251540e-2+y*0.53208e-3))))))
}

// besselK1 is an implementation of the formula function BESSELK.
func (fn *formulaFuncs) besselK1(x formulaArg) float64 {
	var n2, y float64
	if x.Number <= 2 {
		n2 = x.Number * 0.5
		y = n2 * n2
		args := list.New()
		args.PushBack(x)
		args.PushBack(newNumberFormulaArg(1))
		return math.Log(n2)*fn.BESSELI(args).Number +
			(1+y*(0.15443144+y*(-0.67278579+y*(-0.18156897+y*(-0.1919402e-1+y*(-0.110404e-2+y*(-0.4686e-4)))))))/x.Number
	}
	y = 2 / x.Number
	return math.Exp(-x.Number) / math.Sqrt(x.Number) *
		(1.25331414 + y*(0.23498619+y*(-0.3655620e-1+y*(0.1504268e-1+y*(-0.780353e-2+y*
			(0.325614e-2+y*(-0.68245e-3)))))))
}

// besselK2 is an implementation of the formula function BESSELK.
func (fn *formulaFuncs) besselK2(x, n formulaArg) float64 {
	tox, bkm, bk, bkp := 2/x.Number, fn.besselK0(x), fn.besselK1(x), 0.0
	for i := 1.0; i < n.Number; i++ {
		bkp = bkm + i*tox*bk
		bkm = bk
		bk = bkp
	}
	return bk
}

// BESSELY function returns the Bessel function, Yn(x), (also known as the
// Weber function or the Neumann function), for a specified order and value
// of x. The syntax of the function is:
//
//	BESSELY(x,n)
func (fn *formulaFuncs) BESSELY(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BESSELY requires 2 numeric arguments")
	}
	x, n := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Back().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	if n.Type != ArgNumber {
		return n
	}
	if x.Number <= 0 || n.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	var result float64
	switch math.Floor(n.Number) {
	case 0:
		result = fn.besselY0(x)
	case 1:
		result = fn.besselY1(x)
	default:
		result = fn.besselY2(x, n)
	}
	return newNumberFormulaArg(result)
}

// besselY0 is an implementation of the formula function BESSELY.
func (fn *formulaFuncs) besselY0(x formulaArg) float64 {
	var y float64
	if x.Number < 8 {
		y = x.Number * x.Number
		f1 := -2957821389.0 + y*(7062834065.0+y*(-512359803.6+y*(10879881.29+y*
			(-86327.92757+y*228.4622733))))
		f2 := 40076544269.0 + y*(745249964.8+y*(7189466.438+y*
			(47447.26470+y*(226.1030244+y))))
		args := list.New()
		args.PushBack(x)
		args.PushBack(newNumberFormulaArg(0))
		return f1/f2 + 0.636619772*fn.BESSELJ(args).Number*math.Log(x.Number)
	}
	z := 8.0 / x.Number
	y = z * z
	xx := x.Number - 0.785398164
	f1 := 1 + y*(-0.1098628627e-2+y*(0.2734510407e-4+y*(-0.2073370639e-5+y*0.2093887211e-6)))
	f2 := -0.1562499995e-1 + y*(0.1430488765e-3+y*(-0.6911147651e-5+y*(0.7621095161e-6+y*
		(-0.934945152e-7))))
	return math.Sqrt(0.636619772/x.Number) * (math.Sin(xx)*f1 + z*math.Cos(xx)*f2)
}

// besselY1 is an implementation of the formula function BESSELY.
func (fn *formulaFuncs) besselY1(x formulaArg) float64 {
	if x.Number < 8 {
		y := x.Number * x.Number
		f1 := x.Number * (-0.4900604943e13 + y*(0.1275274390e13+y*(-0.5153438139e11+y*
			(0.7349264551e9+y*(-0.4237922726e7+y*0.8511937935e4)))))
		f2 := 0.2499580570e14 + y*(0.4244419664e12+y*(0.3733650367e10+y*(0.2245904002e8+y*
			(0.1020426050e6+y*(0.3549632885e3+y)))))
		args := list.New()
		args.PushBack(x)
		args.PushBack(newNumberFormulaArg(1))
		return f1/f2 + 0.636619772*(fn.BESSELJ(args).Number*math.Log(x.Number)-1/x.Number)
	}
	return math.Sqrt(0.636619772/x.Number) * math.Sin(x.Number-2.356194491)
}

// besselY2 is an implementation of the formula function BESSELY.
func (fn *formulaFuncs) besselY2(x, n formulaArg) float64 {
	tox, bym, by, byp := 2/x.Number, fn.besselY0(x), fn.besselY1(x), 0.0
	for i := 1.0; i < n.Number; i++ {
		byp = i*tox*by - bym
		bym = by
		by = byp
	}
	return by
}

// BIN2DEC function converts a Binary (a base-2 number) into a decimal number.
// The syntax of the function is:
//
//	BIN2DEC(number)
func (fn *formulaFuncs) BIN2DEC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "BIN2DEC requires 1 numeric argument")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	return fn.bin2dec(token.Value())
}

// BIN2HEX function converts a Binary (Base 2) number into a Hexadecimal
// (Base 16) number. The syntax of the function is:
//
//	BIN2HEX(number,[places])
func (fn *formulaFuncs) BIN2HEX(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "BIN2HEX requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BIN2HEX allows at most 2 arguments")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	decimal, newList := fn.bin2dec(token.Value()), list.New()
	if decimal.Type != ArgNumber {
		return decimal
	}
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("BIN2HEX", newList)
}

// BIN2OCT function converts a Binary (Base 2) number into an Octal (Base 8)
// number. The syntax of the function is:
//
//	BIN2OCT(number,[places])
func (fn *formulaFuncs) BIN2OCT(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "BIN2OCT requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BIN2OCT allows at most 2 arguments")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	decimal, newList := fn.bin2dec(token.Value()), list.New()
	if decimal.Type != ArgNumber {
		return decimal
	}
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("BIN2OCT", newList)
}

// bin2dec is an implementation of the formula function BIN2DEC.
func (fn *formulaFuncs) bin2dec(number string) formulaArg {
	decimal, length := 0.0, len(number)
	for i := length; i > 0; i-- {
		s := string(number[length-i])
		if i == 10 && s == "1" {
			decimal += math.Pow(-2.0, float64(i-1))
			continue
		}
		if s == "1" {
			decimal += math.Pow(2.0, float64(i-1))
			continue
		}
		if s != "0" {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newNumberFormulaArg(decimal)
}

// BITAND function returns the bitwise 'AND' for two supplied integers. The
// syntax of the function is:
//
//	BITAND(number1,number2)
func (fn *formulaFuncs) BITAND(argsList *list.List) formulaArg {
	return fn.bitwise("BITAND", argsList)
}

// BITLSHIFT function returns a supplied integer, shifted left by a specified
// number of bits. The syntax of the function is:
//
//	BITLSHIFT(number1,shift_amount)
func (fn *formulaFuncs) BITLSHIFT(argsList *list.List) formulaArg {
	return fn.bitwise("BITLSHIFT", argsList)
}

// BITOR function returns the bitwise 'OR' for two supplied integers. The
// syntax of the function is:
//
//	BITOR(number1,number2)
func (fn *formulaFuncs) BITOR(argsList *list.List) formulaArg {
	return fn.bitwise("BITOR", argsList)
}

// BITRSHIFT function returns a supplied integer, shifted right by a specified
// number of bits. The syntax of the function is:
//
//	BITRSHIFT(number1,shift_amount)
func (fn *formulaFuncs) BITRSHIFT(argsList *list.List) formulaArg {
	return fn.bitwise("BITRSHIFT", argsList)
}

// BITXOR function returns the bitwise 'XOR' (exclusive 'OR') for two supplied
// integers. The syntax of the function is:
//
//	BITXOR(number1,number2)
func (fn *formulaFuncs) BITXOR(argsList *list.List) formulaArg {
	return fn.bitwise("BITXOR", argsList)
}

// bitwise is an implementation of the formula functions BITAND, BITLSHIFT,
// BITOR, BITRSHIFT and BITXOR.
func (fn *formulaFuncs) bitwise(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 numeric arguments", name))
	}
	num1, num2 := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Back().Value.(formulaArg).ToNumber()
	if num1.Type != ArgNumber || num2.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	max := math.Pow(2, 48) - 1
	if num1.Number < 0 || num1.Number > max || num2.Number < 0 || num2.Number > max {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	bitwiseFuncMap := map[string]func(a, b int) int{
		"BITAND":    func(a, b int) int { return a & b },
		"BITLSHIFT": func(a, b int) int { return a << uint(b) },
		"BITOR":     func(a, b int) int { return a | b },
		"BITRSHIFT": func(a, b int) int { return a >> uint(b) },
		"BITXOR":    func(a, b int) int { return a ^ b },
	}
	bitwiseFunc := bitwiseFuncMap[name]
	return newNumberFormulaArg(float64(bitwiseFunc(int(num1.Number), int(num2.Number))))
}

// COMPLEX function takes two arguments, representing the real and the
// imaginary coefficients of a complex number, and from these, creates a
// complex number. The syntax of the function is:
//
//	COMPLEX(real_num,i_num,[suffix])
func (fn *formulaFuncs) COMPLEX(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "COMPLEX requires at least 2 arguments")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "COMPLEX allows at most 3 arguments")
	}
	realNum, i, suffix := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Front().Next().Value.(formulaArg).ToNumber(), "i"
	if realNum.Type != ArgNumber {
		return realNum
	}
	if i.Type != ArgNumber {
		return i
	}
	if argsList.Len() == 3 {
		if suffix = strings.ToLower(argsList.Back().Value.(formulaArg).Value()); suffix != "i" && suffix != "j" {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	return newStringFormulaArg(cmplx2str(complex(realNum.Number, i.Number), suffix))
}

// cmplx2str replace complex number string characters.
func cmplx2str(num complex128, suffix string) string {
	realPart, imagPart := fmt.Sprint(real(num)), fmt.Sprint(imag(num))
	isNum, i, decimal := isNumeric(realPart)
	if isNum && i > 15 {
		realPart = strconv.FormatFloat(decimal, 'G', 15, 64)
	}
	isNum, i, decimal = isNumeric(imagPart)
	if isNum && i > 15 {
		imagPart = strconv.FormatFloat(decimal, 'G', 15, 64)
	}
	c := realPart
	if imag(num) > 0 {
		c += "+"
	}
	if imag(num) != 0 {
		c += imagPart + "i"
	}
	c = strings.TrimPrefix(c, "(")
	c = strings.TrimPrefix(c, "+0+")
	c = strings.TrimPrefix(c, "-0+")
	c = strings.TrimSuffix(c, ")")
	c = strings.TrimPrefix(c, "0+")
	if strings.HasPrefix(c, "0-") {
		c = "-" + strings.TrimPrefix(c, "0-")
	}
	c = strings.TrimPrefix(c, "0+")
	c = strings.TrimSuffix(c, "+0i")
	c = strings.TrimSuffix(c, "-0i")
	c = strings.NewReplacer("+1i", "+i", "-1i", "-i").Replace(c)
	c = strings.ReplaceAll(c, "i", suffix)
	return c
}

// str2cmplx convert complex number string characters.
func str2cmplx(c string) string {
	c = strings.ReplaceAll(c, "j", "i")
	if c == "i" {
		c = "1i"
	}
	c = strings.NewReplacer("+i", "+1i", "-i", "-1i").Replace(c)
	return c
}

// conversionUnit defined unit info for conversion.
type conversionUnit struct {
	group       uint8
	allowPrefix bool
}

// conversionUnits maps info list for unit conversion, that can be used in
// formula function CONVERT.
var conversionUnits = map[string]conversionUnit{
	// weight and mass
	"g":        {group: categoryWeightAndMass, allowPrefix: true},
	"sg":       {group: categoryWeightAndMass, allowPrefix: false},
	"lbm":      {group: categoryWeightAndMass, allowPrefix: false},
	"u":        {group: categoryWeightAndMass, allowPrefix: true},
	"ozm":      {group: categoryWeightAndMass, allowPrefix: false},
	"grain":    {group: categoryWeightAndMass, allowPrefix: false},
	"cwt":      {group: categoryWeightAndMass, allowPrefix: false},
	"shweight": {group: categoryWeightAndMass, allowPrefix: false},
	"uk_cwt":   {group: categoryWeightAndMass, allowPrefix: false},
	"lcwt":     {group: categoryWeightAndMass, allowPrefix: false},
	"hweight":  {group: categoryWeightAndMass, allowPrefix: false},
	"stone":    {group: categoryWeightAndMass, allowPrefix: false},
	"ton":      {group: categoryWeightAndMass, allowPrefix: false},
	"uk_ton":   {group: categoryWeightAndMass, allowPrefix: false},
	"LTON":     {group: categoryWeightAndMass, allowPrefix: false},
	"brton":    {group: categoryWeightAndMass, allowPrefix: false},
	// distance
	"m":         {group: categoryDistance, allowPrefix: true},
	"mi":        {group: categoryDistance, allowPrefix: false},
	"Nmi":       {group: categoryDistance, allowPrefix: false},
	"in":        {group: categoryDistance, allowPrefix: false},
	"ft":        {group: categoryDistance, allowPrefix: false},
	"yd":        {group: categoryDistance, allowPrefix: false},
	"ang":       {group: categoryDistance, allowPrefix: true},
	"ell":       {group: categoryDistance, allowPrefix: false},
	"ly":        {group: categoryDistance, allowPrefix: false},
	"parsec":    {group: categoryDistance, allowPrefix: false},
	"pc":        {group: categoryDistance, allowPrefix: false},
	"Pica":      {group: categoryDistance, allowPrefix: false},
	"Picapt":    {group: categoryDistance, allowPrefix: false},
	"pica":      {group: categoryDistance, allowPrefix: false},
	"survey_mi": {group: categoryDistance, allowPrefix: false},
	// time
	"yr":  {group: categoryTime, allowPrefix: false},
	"day": {group: categoryTime, allowPrefix: false},
	"d":   {group: categoryTime, allowPrefix: false},
	"hr":  {group: categoryTime, allowPrefix: false},
	"mn":  {group: categoryTime, allowPrefix: false},
	"min": {group: categoryTime, allowPrefix: false},
	"sec": {group: categoryTime, allowPrefix: true},
	"s":   {group: categoryTime, allowPrefix: true},
	// pressure
	"Pa":   {group: categoryPressure, allowPrefix: true},
	"p":    {group: categoryPressure, allowPrefix: true},
	"atm":  {group: categoryPressure, allowPrefix: true},
	"at":   {group: categoryPressure, allowPrefix: true},
	"mmHg": {group: categoryPressure, allowPrefix: true},
	"psi":  {group: categoryPressure, allowPrefix: true},
	"Torr": {group: categoryPressure, allowPrefix: true},
	// force
	"N":    {group: categoryForce, allowPrefix: true},
	"dyn":  {group: categoryForce, allowPrefix: true},
	"dy":   {group: categoryForce, allowPrefix: true},
	"lbf":  {group: categoryForce, allowPrefix: false},
	"pond": {group: categoryForce, allowPrefix: true},
	// energy
	"J":   {group: categoryEnergy, allowPrefix: true},
	"e":   {group: categoryEnergy, allowPrefix: true},
	"c":   {group: categoryEnergy, allowPrefix: true},
	"cal": {group: categoryEnergy, allowPrefix: true},
	"eV":  {group: categoryEnergy, allowPrefix: true},
	"ev":  {group: categoryEnergy, allowPrefix: true},
	"HPh": {group: categoryEnergy, allowPrefix: false},
	"hh":  {group: categoryEnergy, allowPrefix: false},
	"Wh":  {group: categoryEnergy, allowPrefix: true},
	"wh":  {group: categoryEnergy, allowPrefix: true},
	"flb": {group: categoryEnergy, allowPrefix: false},
	"BTU": {group: categoryEnergy, allowPrefix: false},
	"btu": {group: categoryEnergy, allowPrefix: false},
	// power
	"HP": {group: categoryPower, allowPrefix: false},
	"h":  {group: categoryPower, allowPrefix: false},
	"W":  {group: categoryPower, allowPrefix: true},
	"w":  {group: categoryPower, allowPrefix: true},
	"PS": {group: categoryPower, allowPrefix: false},
	"T":  {group: categoryMagnetism, allowPrefix: true},
	"ga": {group: categoryMagnetism, allowPrefix: true},
	// temperature
	"C":    {group: categoryTemperature, allowPrefix: false},
	"cel":  {group: categoryTemperature, allowPrefix: false},
	"F":    {group: categoryTemperature, allowPrefix: false},
	"fah":  {group: categoryTemperature, allowPrefix: false},
	"K":    {group: categoryTemperature, allowPrefix: false},
	"kel":  {group: categoryTemperature, allowPrefix: false},
	"Rank": {group: categoryTemperature, allowPrefix: false},
	"Reau": {group: categoryTemperature, allowPrefix: false},
	// volume
	"l":        {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"L":        {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"lt":       {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"tsp":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"tspm":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"tbs":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"oz":       {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"cup":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"pt":       {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"us_pt":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"uk_pt":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"qt":       {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"uk_qt":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"gal":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"uk_gal":   {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"ang3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"ang^3":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"barrel":   {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"bushel":   {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"in3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"in^3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"ft3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"ft^3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"ly3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"ly^3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"m3":       {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"m^3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: true},
	"mi3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"mi^3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"yd3":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"yd^3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Nmi3":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Nmi^3":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Pica3":    {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Pica^3":   {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Picapt3":  {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"Picapt^3": {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"GRT":      {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"regton":   {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	"MTON":     {group: categoryVolumeAndLiquidMeasure, allowPrefix: false},
	// area
	"ha":       {group: categoryArea, allowPrefix: true},
	"uk_acre":  {group: categoryArea, allowPrefix: false},
	"us_acre":  {group: categoryArea, allowPrefix: false},
	"ang2":     {group: categoryArea, allowPrefix: true},
	"ang^2":    {group: categoryArea, allowPrefix: true},
	"ar":       {group: categoryArea, allowPrefix: true},
	"ft2":      {group: categoryArea, allowPrefix: false},
	"ft^2":     {group: categoryArea, allowPrefix: false},
	"in2":      {group: categoryArea, allowPrefix: false},
	"in^2":     {group: categoryArea, allowPrefix: false},
	"ly2":      {group: categoryArea, allowPrefix: false},
	"ly^2":     {group: categoryArea, allowPrefix: false},
	"m2":       {group: categoryArea, allowPrefix: true},
	"m^2":      {group: categoryArea, allowPrefix: true},
	"Morgen":   {group: categoryArea, allowPrefix: false},
	"mi2":      {group: categoryArea, allowPrefix: false},
	"mi^2":     {group: categoryArea, allowPrefix: false},
	"Nmi2":     {group: categoryArea, allowPrefix: false},
	"Nmi^2":    {group: categoryArea, allowPrefix: false},
	"Pica2":    {group: categoryArea, allowPrefix: false},
	"Pica^2":   {group: categoryArea, allowPrefix: false},
	"Picapt2":  {group: categoryArea, allowPrefix: false},
	"Picapt^2": {group: categoryArea, allowPrefix: false},
	"yd2":      {group: categoryArea, allowPrefix: false},
	"yd^2":     {group: categoryArea, allowPrefix: false},
	// information
	"byte": {group: categoryInformation, allowPrefix: true},
	"bit":  {group: categoryInformation, allowPrefix: true},
	// speed
	"m/s":   {group: categorySpeed, allowPrefix: true},
	"m/sec": {group: categorySpeed, allowPrefix: true},
	"m/h":   {group: categorySpeed, allowPrefix: true},
	"m/hr":  {group: categorySpeed, allowPrefix: true},
	"mph":   {group: categorySpeed, allowPrefix: false},
	"admkn": {group: categorySpeed, allowPrefix: false},
	"kn":    {group: categorySpeed, allowPrefix: false},
}

// unitConversions maps details of the Units of measure conversion factors,
// organised by group.
var unitConversions = map[byte]map[string]float64{
	// conversion uses gram (g) as an intermediate unit
	categoryWeightAndMass: {
		"g":        1,
		"sg":       6.85217658567918e-05,
		"lbm":      2.20462262184878e-03,
		"u":        6.02214179421676e+23,
		"ozm":      3.52739619495804e-02,
		"grain":    1.54323583529414e+01,
		"cwt":      2.20462262184878e-05,
		"shweight": 2.20462262184878e-05,
		"uk_cwt":   1.96841305522212e-05,
		"lcwt":     1.96841305522212e-05,
		"hweight":  1.96841305522212e-05,
		"stone":    1.57473044417770e-04,
		"ton":      1.10231131092439e-06,
		"uk_ton":   9.84206527611061e-07,
		"LTON":     9.84206527611061e-07,
		"brton":    9.84206527611061e-07,
	},
	// conversion uses meter (m) as an intermediate unit
	categoryDistance: {
		"m":         1,
		"mi":        6.21371192237334e-04,
		"Nmi":       5.39956803455724e-04,
		"in":        3.93700787401575e+01,
		"ft":        3.28083989501312e+00,
		"yd":        1.09361329833771e+00,
		"ang":       1.0e+10,
		"ell":       8.74890638670166e-01,
		"ly":        1.05700083402462e-16,
		"parsec":    3.24077928966473e-17,
		"pc":        3.24077928966473e-17,
		"Pica":      2.83464566929134e+03,
		"Picapt":    2.83464566929134e+03,
		"pica":      2.36220472440945e+02,
		"survey_mi": 6.21369949494950e-04,
	},
	// conversion uses second (s) as an intermediate unit
	categoryTime: {
		"yr":  3.16880878140289e-08,
		"day": 1.15740740740741e-05,
		"d":   1.15740740740741e-05,
		"hr":  2.77777777777778e-04,
		"mn":  1.66666666666667e-02,
		"min": 1.66666666666667e-02,
		"sec": 1,
		"s":   1,
	},
	// conversion uses Pascal (Pa) as an intermediate unit
	categoryPressure: {
		"Pa":   1,
		"p":    1,
		"atm":  9.86923266716013e-06,
		"at":   9.86923266716013e-06,
		"mmHg": 7.50063755419211e-03,
		"psi":  1.45037737730209e-04,
		"Torr": 7.50061682704170e-03,
	},
	// conversion uses Newton (N) as an intermediate unit
	categoryForce: {
		"N":    1,
		"dyn":  1.0e+5,
		"dy":   1.0e+5,
		"lbf":  2.24808923655339e-01,
		"pond": 1.01971621297793e+02,
	},
	// conversion uses Joule (J) as an intermediate unit
	categoryEnergy: {
		"J":   1,
		"e":   9.99999519343231e+06,
		"c":   2.39006249473467e-01,
		"cal": 2.38846190642017e-01,
		"eV":  6.24145700000000e+18,
		"ev":  6.24145700000000e+18,
		"HPh": 3.72506430801000e-07,
		"hh":  3.72506430801000e-07,
		"Wh":  2.77777916238711e-04,
		"wh":  2.77777916238711e-04,
		"flb": 2.37304222192651e+01,
		"BTU": 9.47815067349015e-04,
		"btu": 9.47815067349015e-04,
	},
	// conversion uses Horsepower (HP) as an intermediate unit
	categoryPower: {
		"HP": 1,
		"h":  1,
		"W":  7.45699871582270e+02,
		"w":  7.45699871582270e+02,
		"PS": 1.01386966542400e+00,
	},
	// conversion uses Tesla (T) as an intermediate unit
	categoryMagnetism: {
		"T":  1,
		"ga": 10000,
	},
	// conversion uses litre (l) as an intermediate unit
	categoryVolumeAndLiquidMeasure: {
		"l":        1,
		"L":        1,
		"lt":       1,
		"tsp":      2.02884136211058e+02,
		"tspm":     2.0e+02,
		"tbs":      6.76280454036860e+01,
		"oz":       3.38140227018430e+01,
		"cup":      4.22675283773038e+00,
		"pt":       2.11337641886519e+00,
		"us_pt":    2.11337641886519e+00,
		"uk_pt":    1.75975398639270e+00,
		"qt":       1.05668820943259e+00,
		"uk_qt":    8.79876993196351e-01,
		"gal":      2.64172052358148e-01,
		"uk_gal":   2.19969248299088e-01,
		"ang3":     1.0e+27,
		"ang^3":    1.0e+27,
		"barrel":   6.28981077043211e-03,
		"bushel":   2.83775932584017e-02,
		"in3":      6.10237440947323e+01,
		"in^3":     6.10237440947323e+01,
		"ft3":      3.53146667214886e-02,
		"ft^3":     3.53146667214886e-02,
		"ly3":      1.18093498844171e-51,
		"ly^3":     1.18093498844171e-51,
		"m3":       1.0e-03,
		"m^3":      1.0e-03,
		"mi3":      2.39912758578928e-13,
		"mi^3":     2.39912758578928e-13,
		"yd3":      1.30795061931439e-03,
		"yd^3":     1.30795061931439e-03,
		"Nmi3":     1.57426214685811e-13,
		"Nmi^3":    1.57426214685811e-13,
		"Pica3":    2.27769904358706e+07,
		"Pica^3":   2.27769904358706e+07,
		"Picapt3":  2.27769904358706e+07,
		"Picapt^3": 2.27769904358706e+07,
		"GRT":      3.53146667214886e-04,
		"regton":   3.53146667214886e-04,
		"MTON":     8.82866668037215e-04,
	},
	// conversion uses hectare (ha) as an intermediate unit
	categoryArea: {
		"ha":       1,
		"uk_acre":  2.47105381467165e+00,
		"us_acre":  2.47104393046628e+00,
		"ang2":     1.0e+24,
		"ang^2":    1.0e+24,
		"ar":       1.0e+02,
		"ft2":      1.07639104167097e+05,
		"ft^2":     1.07639104167097e+05,
		"in2":      1.55000310000620e+07,
		"in^2":     1.55000310000620e+07,
		"ly2":      1.11725076312873e-28,
		"ly^2":     1.11725076312873e-28,
		"m2":       1.0e+04,
		"m^2":      1.0e+04,
		"Morgen":   4.0e+00,
		"mi2":      3.86102158542446e-03,
		"mi^2":     3.86102158542446e-03,
		"Nmi2":     2.91553349598123e-03,
		"Nmi^2":    2.91553349598123e-03,
		"Pica2":    8.03521607043214e+10,
		"Pica^2":   8.03521607043214e+10,
		"Picapt2":  8.03521607043214e+10,
		"Picapt^2": 8.03521607043214e+10,
		"yd2":      1.19599004630108e+04,
		"yd^2":     1.19599004630108e+04,
	},
	// conversion uses bit (bit) as an intermediate unit
	categoryInformation: {
		"bit":  1,
		"byte": 0.125,
	},
	// conversion uses Meters per Second (m/s) as an intermediate unit
	categorySpeed: {
		"m/s":   1,
		"m/sec": 1,
		"m/h":   3.60e+03,
		"m/hr":  3.60e+03,
		"mph":   2.23693629205440e+00,
		"admkn": 1.94260256941567e+00,
		"kn":    1.94384449244060e+00,
	},
}

// conversionMultipliers maps details of the Multiplier prefixes that can be
// used with Units of Measure in CONVERT.
var conversionMultipliers = map[string]float64{
	"Y":  1e24,
	"Z":  1e21,
	"E":  1e18,
	"P":  1e15,
	"T":  1e12,
	"G":  1e9,
	"M":  1e6,
	"k":  1e3,
	"h":  1e2,
	"e":  1e1,
	"da": 1e1,
	"d":  1e-1,
	"c":  1e-2,
	"m":  1e-3,
	"u":  1e-6,
	"n":  1e-9,
	"p":  1e-12,
	"f":  1e-15,
	"a":  1e-18,
	"z":  1e-21,
	"y":  1e-24,
	"Yi": math.Pow(2, 80),
	"Zi": math.Pow(2, 70),
	"Ei": math.Pow(2, 60),
	"Pi": math.Pow(2, 50),
	"Ti": math.Pow(2, 40),
	"Gi": math.Pow(2, 30),
	"Mi": math.Pow(2, 20),
	"ki": math.Pow(2, 10),
}

// getUnitDetails check and returns the unit of measure details.
func getUnitDetails(uom string) (unit string, catgory byte, res float64, ok bool) {
	if len(uom) == 0 {
		ok = false
		return
	}
	if unit, ok := conversionUnits[uom]; ok {
		return uom, unit.group, 1, ok
	}
	// 1 character standard metric multiplier prefixes
	multiplierType := uom[:1]
	uom = uom[1:]
	conversionUnit, ok1 := conversionUnits[uom]
	multiplier, ok2 := conversionMultipliers[multiplierType]
	if ok1 && ok2 {
		if !conversionUnit.allowPrefix {
			ok = false
			return
		}
		unitCategory := conversionUnit.group
		return uom, unitCategory, multiplier, true
	}
	// 2 character standard and binary metric multiplier prefixes
	if len(uom) > 0 {
		multiplierType += uom[:1]
		uom = uom[1:]
	}
	conversionUnit, ok1 = conversionUnits[uom]
	multiplier, ok2 = conversionMultipliers[multiplierType]
	if ok1 && ok2 {
		if !conversionUnit.allowPrefix {
			ok = false
			return
		}
		unitCategory := conversionUnit.group
		return uom, unitCategory, multiplier, true
	}
	ok = false
	return
}

// resolveTemperatureSynonyms returns unit of measure according to a given
// temperature synonyms.
func resolveTemperatureSynonyms(uom string) string {
	switch uom {
	case "fah":
		return "F"
	case "cel":
		return "C"
	case "kel":
		return "K"
	}
	return uom
}

// convertTemperature returns converted temperature by a given unit of measure.
func convertTemperature(fromUOM, toUOM string, value float64) float64 {
	fromUOM = resolveTemperatureSynonyms(fromUOM)
	toUOM = resolveTemperatureSynonyms(toUOM)
	if fromUOM == toUOM {
		return value
	}
	// convert to Kelvin
	switch fromUOM {
	case "F":
		value = (value-32)/1.8 + 273.15
	case "C":
		value += 273.15
	case "Rank":
		value /= 1.8
	case "Reau":
		value = value*1.25 + 273.15
	}
	// convert from Kelvin
	switch toUOM {
	case "F":
		value = (value-273.15)*1.8 + 32
	case "C":
		value -= 273.15
	case "Rank":
		value *= 1.8
	case "Reau":
		value = (value - 273.15) * 0.8
	}
	return value
}

// CONVERT function converts a number from one unit type (e.g. Yards) to
// another unit type (e.g. Meters). The syntax of the function is:
//
//	CONVERT(number,from_unit,to_unit)
func (fn *formulaFuncs) CONVERT(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "CONVERT requires 3 arguments")
	}
	num := argsList.Front().Value.(formulaArg).ToNumber()
	if num.Type != ArgNumber {
		return num
	}
	fromUOM, fromCategory, fromMultiplier, ok1 := getUnitDetails(argsList.Front().Next().Value.(formulaArg).Value())
	toUOM, toCategory, toMultiplier, ok2 := getUnitDetails(argsList.Back().Value.(formulaArg).Value())
	if !ok1 || !ok2 || fromCategory != toCategory {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	val := num.Number * fromMultiplier
	if fromUOM == toUOM && fromMultiplier == toMultiplier {
		return newNumberFormulaArg(val / fromMultiplier)
	} else if fromUOM == toUOM {
		return newNumberFormulaArg(val / toMultiplier)
	} else if fromCategory == categoryTemperature {
		return newNumberFormulaArg(convertTemperature(fromUOM, toUOM, val))
	}
	fromConversion := unitConversions[fromCategory][fromUOM]
	toConversion := unitConversions[fromCategory][toUOM]
	baseValue := val * (1 / fromConversion)
	return newNumberFormulaArg((baseValue * toConversion) / toMultiplier)
}

// DEC2BIN function converts a decimal number into a Binary (Base 2) number.
// The syntax of the function is:
//
//	DEC2BIN(number,[places])
func (fn *formulaFuncs) DEC2BIN(argsList *list.List) formulaArg {
	return fn.dec2x("DEC2BIN", argsList)
}

// DEC2HEX function converts a decimal number into a Hexadecimal (Base 16)
// number. The syntax of the function is:
//
//	DEC2HEX(number,[places])
func (fn *formulaFuncs) DEC2HEX(argsList *list.List) formulaArg {
	return fn.dec2x("DEC2HEX", argsList)
}

// DEC2OCT function converts a decimal number into an Octal (Base 8) number.
// The syntax of the function is:
//
//	DEC2OCT(number,[places])
func (fn *formulaFuncs) DEC2OCT(argsList *list.List) formulaArg {
	return fn.dec2x("DEC2OCT", argsList)
}

// dec2x is an implementation of the formula functions DEC2BIN, DEC2HEX and
// DEC2OCT.
func (fn *formulaFuncs) dec2x(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 2 arguments", name))
	}
	decimal := argsList.Front().Value.(formulaArg).ToNumber()
	if decimal.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, decimal.Error)
	}
	maxLimitMap := map[string]float64{
		"DEC2BIN": 511,
		"HEX2BIN": 511,
		"OCT2BIN": 511,
		"BIN2HEX": 549755813887,
		"DEC2HEX": 549755813887,
		"OCT2HEX": 549755813887,
		"BIN2OCT": 536870911,
		"DEC2OCT": 536870911,
		"HEX2OCT": 536870911,
	}
	minLimitMap := map[string]float64{
		"DEC2BIN": -512,
		"HEX2BIN": -512,
		"OCT2BIN": -512,
		"BIN2HEX": -549755813888,
		"DEC2HEX": -549755813888,
		"OCT2HEX": -549755813888,
		"BIN2OCT": -536870912,
		"DEC2OCT": -536870912,
		"HEX2OCT": -536870912,
	}
	baseMap := map[string]int{
		"DEC2BIN": 2,
		"HEX2BIN": 2,
		"OCT2BIN": 2,
		"BIN2HEX": 16,
		"DEC2HEX": 16,
		"OCT2HEX": 16,
		"BIN2OCT": 8,
		"DEC2OCT": 8,
		"HEX2OCT": 8,
	}
	maxLimit, minLimit := maxLimitMap[name], minLimitMap[name]
	base := baseMap[name]
	if decimal.Number < minLimit || decimal.Number > maxLimit {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	n := int64(decimal.Number)
	binary := strconv.FormatUint(*(*uint64)(unsafe.Pointer(&n)), base)
	if argsList.Len() == 2 {
		places := argsList.Back().Value.(formulaArg).ToNumber()
		if places.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, places.Error)
		}
		binaryPlaces := len(binary)
		if places.Number < 0 || places.Number > 10 || binaryPlaces > int(places.Number) {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		return newStringFormulaArg(strings.ToUpper(fmt.Sprintf("%s%s", strings.Repeat("0", int(places.Number)-binaryPlaces), binary)))
	}
	if decimal.Number < 0 && len(binary) > 10 {
		return newStringFormulaArg(strings.ToUpper(binary[len(binary)-10:]))
	}
	return newStringFormulaArg(strings.ToUpper(binary))
}

// DELTA function tests two numbers for equality and returns the Kronecker
// Delta. i.e. the function returns 1 if the two supplied numbers are equal
// and 0 otherwise. The syntax of the function is:
//
//	DELTA(number1,[number2])
func (fn *formulaFuncs) DELTA(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "DELTA requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "DELTA allows at most 2 arguments")
	}
	number1 := argsList.Front().Value.(formulaArg).ToNumber()
	if number1.Type != ArgNumber {
		return number1
	}
	number2 := newNumberFormulaArg(0)
	if argsList.Len() == 2 {
		if number2 = argsList.Back().Value.(formulaArg).ToNumber(); number2.Type != ArgNumber {
			return number2
		}
	}
	return newBoolFormulaArg(number1.Number == number2.Number).ToNumber()
}

// ERF function calculates the Error Function, integrated between two supplied
// limits. The syntax of the function is:
//
//	ERF(lower_limit,[upper_limit])
func (fn *formulaFuncs) ERF(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ERF requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ERF allows at most 2 arguments")
	}
	lower := argsList.Front().Value.(formulaArg).ToNumber()
	if lower.Type != ArgNumber {
		return lower
	}
	if argsList.Len() == 2 {
		upper := argsList.Back().Value.(formulaArg).ToNumber()
		if upper.Type != ArgNumber {
			return upper
		}
		return newNumberFormulaArg(math.Erf(upper.Number) - math.Erf(lower.Number))
	}
	return newNumberFormulaArg(math.Erf(lower.Number))
}

// ERFdotPRECISE function calculates the Error Function, integrated between a
// supplied lower or upper limit and 0. The syntax of the function is:
//
//	ERF.PRECISE(x)
func (fn *formulaFuncs) ERFdotPRECISE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ERF.PRECISE requires 1 argument")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	return newNumberFormulaArg(math.Erf(x.Number))
}

// erfc is an implementation of the formula functions ERFC and ERFC.PRECISE.
func (fn *formulaFuncs) erfc(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 1 argument", name))
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	return newNumberFormulaArg(math.Erfc(x.Number))
}

// ERFC function calculates the Complementary Error Function, integrated
// between a supplied lower limit and infinity. The syntax of the function
// is:
//
//	ERFC(x)
func (fn *formulaFuncs) ERFC(argsList *list.List) formulaArg {
	return fn.erfc("ERFC", argsList)
}

// ERFCdotPRECISE function calculates the Complementary Error Function,
// integrated between a supplied lower limit and infinity. The syntax of the
// function is:
//
//	ERFC(x)
func (fn *formulaFuncs) ERFCdotPRECISE(argsList *list.List) formulaArg {
	return fn.erfc("ERFC.PRECISE", argsList)
}

// GESTEP unction tests whether a supplied number is greater than a supplied
// step size and returns. The syntax of the function is:
//
//	GESTEP(number,[step])
func (fn *formulaFuncs) GESTEP(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GESTEP requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "GESTEP allows at most 2 arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type != ArgNumber {
		return number
	}
	step := newNumberFormulaArg(0)
	if argsList.Len() == 2 {
		if step = argsList.Back().Value.(formulaArg).ToNumber(); step.Type != ArgNumber {
			return step
		}
	}
	return newBoolFormulaArg(number.Number >= step.Number).ToNumber()
}

// HEX2BIN function converts a Hexadecimal (Base 16) number into a Binary
// (Base 2) number. The syntax of the function is:
//
//	HEX2BIN(number,[places])
func (fn *formulaFuncs) HEX2BIN(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HEX2BIN requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "HEX2BIN allows at most 2 arguments")
	}
	decimal, newList := fn.hex2dec(argsList.Front().Value.(formulaArg).Value()), list.New()
	if decimal.Type != ArgNumber {
		return decimal
	}
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("HEX2BIN", newList)
}

// HEX2DEC function converts a hexadecimal (a base-16 number) into a decimal
// number. The syntax of the function is:
//
//	HEX2DEC(number)
func (fn *formulaFuncs) HEX2DEC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HEX2DEC requires 1 numeric argument")
	}
	return fn.hex2dec(argsList.Front().Value.(formulaArg).Value())
}

// HEX2OCT function converts a Hexadecimal (Base 16) number into an Octal
// (Base 8) number. The syntax of the function is:
//
//	HEX2OCT(number,[places])
func (fn *formulaFuncs) HEX2OCT(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HEX2OCT requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "HEX2OCT allows at most 2 arguments")
	}
	decimal, newList := fn.hex2dec(argsList.Front().Value.(formulaArg).Value()), list.New()
	if decimal.Type != ArgNumber {
		return decimal
	}
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("HEX2OCT", newList)
}

// hex2dec is an implementation of the formula function HEX2DEC.
func (fn *formulaFuncs) hex2dec(number string) formulaArg {
	decimal, length := 0.0, len(number)
	for i := length; i > 0; i-- {
		num, err := strconv.ParseInt(string(number[length-i]), 16, 64)
		if err != nil {
			return newErrorFormulaArg(formulaErrorNUM, err.Error())
		}
		if i == 10 && string(number[length-i]) == "F" {
			decimal += math.Pow(-16.0, float64(i-1))
			continue
		}
		decimal += float64(num) * math.Pow(16.0, float64(i-1))
	}
	return newNumberFormulaArg(decimal)
}

// IMABS function returns the absolute value (the modulus) of a complex
// number. The syntax of the function is:
//
//	IMABS(inumber)
func (fn *formulaFuncs) IMABS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMABS requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newNumberFormulaArg(cmplx.Abs(inumber))
}

// IMAGINARY function returns the imaginary coefficient of a supplied complex
// number. The syntax of the function is:
//
//	IMAGINARY(inumber)
func (fn *formulaFuncs) IMAGINARY(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMAGINARY requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newNumberFormulaArg(imag(inumber))
}

// IMARGUMENT function returns the phase (also called the argument) of a
// supplied complex number. The syntax of the function is:
//
//	IMARGUMENT(inumber)
func (fn *formulaFuncs) IMARGUMENT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMARGUMENT requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newNumberFormulaArg(cmplx.Phase(inumber))
}

// IMCONJUGATE function returns the complex conjugate of a supplied complex
// number. The syntax of the function is:
//
//	IMCONJUGATE(inumber)
func (fn *formulaFuncs) IMCONJUGATE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCONJUGATE requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Conj(inumber), value[len(value)-1:]))
}

// IMCOS function returns the cosine of a supplied complex number. The syntax
// of the function is:
//
//	IMCOS(inumber)
func (fn *formulaFuncs) IMCOS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCOS requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Cos(inumber), value[len(value)-1:]))
}

// IMCOSH function returns the hyperbolic cosine of a supplied complex number. The syntax
// of the function is:
//
//	IMCOSH(inumber)
func (fn *formulaFuncs) IMCOSH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCOSH requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Cosh(inumber), value[len(value)-1:]))
}

// IMCOT function returns the cotangent of a supplied complex number. The syntax
// of the function is:
//
//	IMCOT(inumber)
func (fn *formulaFuncs) IMCOT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCOT requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Cot(inumber), value[len(value)-1:]))
}

// IMCSC function returns the cosecant of a supplied complex number. The syntax
// of the function is:
//
//	IMCSC(inumber)
func (fn *formulaFuncs) IMCSC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCSC requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := 1 / cmplx.Sin(inumber)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMCSCH function returns the hyperbolic cosecant of a supplied complex
// number. The syntax of the function is:
//
//	IMCSCH(inumber)
func (fn *formulaFuncs) IMCSCH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMCSCH requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := 1 / cmplx.Sinh(inumber)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMDIV function calculates the quotient of two complex numbers (i.e. divides
// one complex number by another). The syntax of the function is:
//
//	IMDIV(inumber1,inumber2)
func (fn *formulaFuncs) IMDIV(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMDIV requires 2 arguments")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber1, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	inumber2, err := strconv.ParseComplex(str2cmplx(argsList.Back().Value.(formulaArg).Value()), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := inumber1 / inumber2
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMEXP function returns the exponential of a supplied complex number. The
// syntax of the function is:
//
//	IMEXP(inumber)
func (fn *formulaFuncs) IMEXP(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMEXP requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Exp(inumber), value[len(value)-1:]))
}

// IMLN function returns the natural logarithm of a supplied complex number.
// The syntax of the function is:
//
//	IMLN(inumber)
func (fn *formulaFuncs) IMLN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMLN requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := cmplx.Log(inumber)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMLOG10 function returns the common (base 10) logarithm of a supplied
// complex number. The syntax of the function is:
//
//	IMLOG10(inumber)
func (fn *formulaFuncs) IMLOG10(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMLOG10 requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := cmplx.Log10(inumber)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMLOG2 function calculates the base 2 logarithm of a supplied complex
// number. The syntax of the function is:
//
//	IMLOG2(inumber)
func (fn *formulaFuncs) IMLOG2(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMLOG2 requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	num := cmplx.Log(inumber)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num/cmplx.Log(2), value[len(value)-1:]))
}

// IMPOWER function returns a supplied complex number, raised to a given
// power. The syntax of the function is:
//
//	IMPOWER(inumber,number)
func (fn *formulaFuncs) IMPOWER(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMPOWER requires 2 arguments")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	number, err := strconv.ParseComplex(str2cmplx(argsList.Back().Value.(formulaArg).Value()), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	if inumber == 0 && number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	num := cmplx.Pow(inumber, number)
	if cmplx.IsInf(num) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newStringFormulaArg(cmplx2str(num, value[len(value)-1:]))
}

// IMPRODUCT function calculates the product of two or more complex numbers.
// The syntax of the function is:
//
//	IMPRODUCT(number1,[number2],...)
func (fn *formulaFuncs) IMPRODUCT(argsList *list.List) formulaArg {
	product := complex128(1)
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			if token.Value() == "" {
				continue
			}
			val, err := strconv.ParseComplex(str2cmplx(token.Value()), 128)
			if err != nil {
				return newErrorFormulaArg(formulaErrorNUM, err.Error())
			}
			product = product * val
		case ArgNumber:
			product = product * complex(token.Number, 0)
		case ArgMatrix:
			for _, row := range token.Matrix {
				for _, value := range row {
					if value.Value() == "" {
						continue
					}
					val, err := strconv.ParseComplex(str2cmplx(value.Value()), 128)
					if err != nil {
						return newErrorFormulaArg(formulaErrorNUM, err.Error())
					}
					product = product * val
				}
			}
		}
	}
	return newStringFormulaArg(cmplx2str(product, "i"))
}

// IMREAL function returns the real coefficient of a supplied complex number.
// The syntax of the function is:
//
//	IMREAL(inumber)
func (fn *formulaFuncs) IMREAL(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMREAL requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(fmt.Sprint(real(inumber)))
}

// IMSEC function returns the secant of a supplied complex number. The syntax
// of the function is:
//
//	IMSEC(inumber)
func (fn *formulaFuncs) IMSEC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSEC requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(1/cmplx.Cos(inumber), value[len(value)-1:]))
}

// IMSECH function returns the hyperbolic secant of a supplied complex number.
// The syntax of the function is:
//
//	IMSECH(inumber)
func (fn *formulaFuncs) IMSECH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSECH requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(1/cmplx.Cosh(inumber), value[len(value)-1:]))
}

// IMSIN function returns the Sine of a supplied complex number. The syntax of
// the function is:
//
//	IMSIN(inumber)
func (fn *formulaFuncs) IMSIN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSIN requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Sin(inumber), value[len(value)-1:]))
}

// IMSINH function returns the hyperbolic sine of a supplied complex number.
// The syntax of the function is:
//
//	IMSINH(inumber)
func (fn *formulaFuncs) IMSINH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSINH requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Sinh(inumber), value[len(value)-1:]))
}

// IMSQRT function returns the square root of a supplied complex number. The
// syntax of the function is:
//
//	IMSQRT(inumber)
func (fn *formulaFuncs) IMSQRT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSQRT requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Sqrt(inumber), value[len(value)-1:]))
}

// IMSUB function calculates the difference between two complex numbers
// (i.e. subtracts one complex number from another). The syntax of the
// function is:
//
//	IMSUB(inumber1,inumber2)
func (fn *formulaFuncs) IMSUB(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSUB requires 2 arguments")
	}
	i1, err := strconv.ParseComplex(str2cmplx(argsList.Front().Value.(formulaArg).Value()), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	i2, err := strconv.ParseComplex(str2cmplx(argsList.Back().Value.(formulaArg).Value()), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(i1-i2, "i"))
}

// IMSUM function calculates the sum of two or more complex numbers. The
// syntax of the function is:
//
//	IMSUM(inumber1,inumber2,...)
func (fn *formulaFuncs) IMSUM(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMSUM requires at least 1 argument")
	}
	var result complex128
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		num, err := strconv.ParseComplex(str2cmplx(token.Value()), 128)
		if err != nil {
			return newErrorFormulaArg(formulaErrorNUM, err.Error())
		}
		result += num
	}
	return newStringFormulaArg(cmplx2str(result, "i"))
}

// IMTAN function returns the tangent of a supplied complex number. The syntax
// of the function is:
//
//	IMTAN(inumber)
func (fn *formulaFuncs) IMTAN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IMTAN requires 1 argument")
	}
	value := argsList.Front().Value.(formulaArg).Value()
	inumber, err := strconv.ParseComplex(str2cmplx(value), 128)
	if err != nil {
		return newErrorFormulaArg(formulaErrorNUM, err.Error())
	}
	return newStringFormulaArg(cmplx2str(cmplx.Tan(inumber), value[len(value)-1:]))
}

// OCT2BIN function converts an Octal (Base 8) number into a Binary (Base 2)
// number. The syntax of the function is:
//
//	OCT2BIN(number,[places])
func (fn *formulaFuncs) OCT2BIN(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "OCT2BIN requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "OCT2BIN allows at most 2 arguments")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	decimal, newList := fn.oct2dec(token.Value()), list.New()
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("OCT2BIN", newList)
}

// OCT2DEC function converts an Octal (a base-8 number) into a decimal number.
// The syntax of the function is:
//
//	OCT2DEC(number)
func (fn *formulaFuncs) OCT2DEC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "OCT2DEC requires 1 numeric argument")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	return fn.oct2dec(token.Value())
}

// OCT2HEX function converts an Octal (Base 8) number into a Hexadecimal
// (Base 16) number. The syntax of the function is:
//
//	OCT2HEX(number,[places])
func (fn *formulaFuncs) OCT2HEX(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "OCT2HEX requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "OCT2HEX allows at most 2 arguments")
	}
	token := argsList.Front().Value.(formulaArg)
	number := token.ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, number.Error)
	}
	decimal, newList := fn.oct2dec(token.Value()), list.New()
	newList.PushBack(decimal)
	if argsList.Len() == 2 {
		newList.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.dec2x("OCT2HEX", newList)
}

// oct2dec is an implementation of the formula function OCT2DEC.
func (fn *formulaFuncs) oct2dec(number string) formulaArg {
	decimal, length := 0.0, len(number)
	for i := length; i > 0; i-- {
		num, _ := strconv.Atoi(string(number[length-i]))
		if i == 10 && string(number[length-i]) == "7" {
			decimal += math.Pow(-8.0, float64(i-1))
			continue
		}
		decimal += float64(num) * math.Pow(8.0, float64(i-1))
	}
	return newNumberFormulaArg(decimal)
}

// Math and Trigonometric Functions

// ABS function returns the absolute value of any supplied number. The syntax
// of the function is:
//
//	ABS(number)
func (fn *formulaFuncs) ABS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ABS requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Abs(arg.Number))
}

// ACOS function calculates the arccosine (i.e. the inverse cosine) of a given
// number, and returns an angle, in radians, between 0 and π. The syntax of
// the function is:
//
//	ACOS(number)
func (fn *formulaFuncs) ACOS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACOS requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Acos(arg.Number))
}

// ACOSH function calculates the inverse hyperbolic cosine of a supplied number.
// of the function is:
//
//	ACOSH(number)
func (fn *formulaFuncs) ACOSH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACOSH requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Acosh(arg.Number))
}

// ACOT function calculates the arccotangent (i.e. the inverse cotangent) of a
// given number, and returns an angle, in radians, between 0 and π. The syntax
// of the function is:
//
//	ACOT(number)
func (fn *formulaFuncs) ACOT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACOT requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Pi/2 - math.Atan(arg.Number))
}

// ACOTH function calculates the hyperbolic arccotangent (coth) of a supplied
// value. The syntax of the function is:
//
//	ACOTH(number)
func (fn *formulaFuncs) ACOTH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACOTH requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Atanh(1 / arg.Number))
}

// AGGREGATE function returns the result of a specified operation or function,
// applied to a list or database of values. The syntax of the function is:
//
//	AGGREGATE(function_num,options,ref1,[ref2],...)
func (fn *formulaFuncs) AGGREGATE(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "AGGREGATE requires at least 3 arguments")
	}
	var fnNum, opts formulaArg
	if fnNum = argsList.Front().Value.(formulaArg).ToNumber(); fnNum.Type != ArgNumber {
		return fnNum
	}
	subFn, ok := map[int]func(argsList *list.List) formulaArg{
		1:  fn.AVERAGE,
		2:  fn.COUNT,
		3:  fn.COUNTA,
		4:  fn.MAX,
		5:  fn.MIN,
		6:  fn.PRODUCT,
		7:  fn.STDEVdotS,
		8:  fn.STDEVdotP,
		9:  fn.SUM,
		10: fn.VARdotS,
		11: fn.VARdotP,
		12: fn.MEDIAN,
		13: fn.MODEdotSNGL,
		14: fn.LARGE,
		15: fn.SMALL,
		16: fn.PERCENTILEdotINC,
		17: fn.QUARTILEdotINC,
		18: fn.PERCENTILEdotEXC,
		19: fn.QUARTILEdotEXC,
	}[int(fnNum.Number)]
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, "AGGREGATE has invalid function_num")
	}
	if opts = argsList.Front().Next().Value.(formulaArg).ToNumber(); opts.Type != ArgNumber {
		return opts
	}
	// TODO: apply option argument values to be ignored during the calculation
	if int(opts.Number) < 0 || int(opts.Number) > 7 {
		return newErrorFormulaArg(formulaErrorVALUE, "AGGREGATE has invalid options")
	}
	subArgList := list.New().Init()
	for arg := argsList.Front().Next().Next(); arg != nil; arg = arg.Next() {
		subArgList.PushBack(arg.Value.(formulaArg))
	}
	return subFn(subArgList)
}

// ARABIC function converts a Roman numeral into an Arabic numeral. The syntax
// of the function is:
//
//	ARABIC(text)
func (fn *formulaFuncs) ARABIC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ARABIC requires 1 numeric argument")
	}
	text := argsList.Front().Value.(formulaArg).Value()
	if len(text) > MaxFieldLength {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	text = strings.ToUpper(text)
	number, actualStart, index, isNegative := 0, 0, len(text)-1, false
	startIndex, subtractNumber, currentPartValue, currentCharValue, prevCharValue := 0, 0, 0, 0, -1
	for index >= 0 && text[index] == ' ' {
		index--
	}
	for actualStart <= index && text[actualStart] == ' ' {
		actualStart++
	}
	if actualStart <= index && text[actualStart] == '-' {
		isNegative = true
		actualStart++
	}
	charMap := map[rune]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	for index >= actualStart {
		startIndex = index
		startChar := text[startIndex]
		index--
		for index >= actualStart && (text[index]|' ') == startChar {
			index--
		}
		currentCharValue = charMap[rune(startChar)]
		currentPartValue = (startIndex - index) * currentCharValue
		if currentCharValue >= prevCharValue {
			number += currentPartValue - subtractNumber
			prevCharValue = currentCharValue
			subtractNumber = 0
			continue
		}
		subtractNumber += currentPartValue
	}
	if subtractNumber != 0 {
		number -= subtractNumber
	}
	if isNegative {
		number = -number
	}
	return newNumberFormulaArg(float64(number))
}

// ASIN function calculates the arcsine (i.e. the inverse sine) of a given
// number, and returns an angle, in radians, between -π/2 and π/2. The syntax
// of the function is:
//
//	ASIN(number)
func (fn *formulaFuncs) ASIN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ASIN requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Asin(arg.Number))
}

// ASINH function calculates the inverse hyperbolic sine of a supplied number.
// The syntax of the function is:
//
//	ASINH(number)
func (fn *formulaFuncs) ASINH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ASINH requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Asinh(arg.Number))
}

// ATAN function calculates the arctangent (i.e. the inverse tangent) of a
// given number, and returns an angle, in radians, between -π/2 and +π/2. The
// syntax of the function is:
//
//	ATAN(number)
func (fn *formulaFuncs) ATAN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ATAN requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Atan(arg.Number))
}

// ATANH function calculates the inverse hyperbolic tangent of a supplied
// number. The syntax of the function is:
//
//	ATANH(number)
func (fn *formulaFuncs) ATANH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ATANH requires 1 numeric argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type == ArgError {
		return arg
	}
	return newNumberFormulaArg(math.Atanh(arg.Number))
}

// ATAN2 function calculates the arctangent (i.e. the inverse tangent) of a
// given set of x and y coordinates, and returns an angle, in radians, between
// -π/2 and +π/2. The syntax of the function is:
//
//	ATAN2(x_num,y_num)
func (fn *formulaFuncs) ATAN2(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ATAN2 requires 2 numeric arguments")
	}
	x := argsList.Back().Value.(formulaArg).ToNumber()
	if x.Type == ArgError {
		return x
	}
	y := argsList.Front().Value.(formulaArg).ToNumber()
	if y.Type == ArgError {
		return y
	}
	return newNumberFormulaArg(math.Atan2(x.Number, y.Number))
}

// BASE function converts a number into a supplied base (radix), and returns a
// text representation of the calculated value. The syntax of the function is:
//
//	BASE(number,radix,[min_length])
func (fn *formulaFuncs) BASE(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "BASE requires at least 2 arguments")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "BASE allows at most 3 arguments")
	}
	var minLength int
	var err error
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	radix := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if radix.Type == ArgError {
		return radix
	}
	if int(radix.Number) < 2 || int(radix.Number) > 36 {
		return newErrorFormulaArg(formulaErrorVALUE, "radix must be an integer >= 2 and <= 36")
	}
	if argsList.Len() > 2 {
		if minLength, err = strconv.Atoi(argsList.Back().Value.(formulaArg).Value()); err != nil {
			return newErrorFormulaArg(formulaErrorVALUE, err.Error())
		}
	}
	result := strconv.FormatInt(int64(number.Number), int(radix.Number))
	if len(result) < minLength {
		result = strings.Repeat("0", minLength-len(result)) + result
	}
	return newStringFormulaArg(strings.ToUpper(result))
}

// CEILING function rounds a supplied number away from zero, to the nearest
// multiple of a given number. The syntax of the function is:
//
//	CEILING(number,significance)
func (fn *formulaFuncs) CEILING(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING allows at most 2 arguments")
	}
	number, significance, res := 0.0, 1.0, 0.0
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	number = n.Number
	if number < 0 {
		significance = -1
	}
	if argsList.Len() > 1 {
		s := argsList.Back().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
	}
	if significance < 0 && number > 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "negative sig to CEILING invalid")
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Ceil(number))
	}
	number, res = math.Modf(number / significance)
	if res > 0 {
		number++
	}
	return newNumberFormulaArg(number * significance)
}

// CEILINGdotMATH function rounds a supplied number up to a supplied multiple
// of significance. The syntax of the function is:
//
//	CEILING.MATH(number,[significance],[mode])
func (fn *formulaFuncs) CEILINGdotMATH(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING.MATH requires at least 1 argument")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING.MATH allows at most 3 arguments")
	}
	number, significance, mode := 0.0, 1.0, 1.0
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	number = n.Number
	if number < 0 {
		significance = -1
	}
	if argsList.Len() > 1 {
		s := argsList.Front().Next().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Ceil(number))
	}
	if argsList.Len() > 2 {
		m := argsList.Back().Value.(formulaArg).ToNumber()
		if m.Type == ArgError {
			return m
		}
		mode = m.Number
	}
	val, res := math.Modf(number / significance)
	if res != 0 {
		if number > 0 {
			val++
		} else if mode < 0 {
			val--
		}
	}
	return newNumberFormulaArg(val * significance)
}

// CEILINGdotPRECISE function rounds a supplied number up (regardless of the
// number's sign), to the nearest multiple of a given number. The syntax of
// the function is:
//
//	CEILING.PRECISE(number,[significance])
func (fn *formulaFuncs) CEILINGdotPRECISE(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING.PRECISE requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CEILING.PRECISE allows at most 2 arguments")
	}
	number, significance := 0.0, 1.0
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	number = n.Number
	if number < 0 {
		significance = -1
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Ceil(number))
	}
	if argsList.Len() > 1 {
		s := argsList.Back().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
		significance = math.Abs(significance)
		if significance == 0 {
			return newNumberFormulaArg(significance)
		}
	}
	val, res := math.Modf(number / significance)
	if res != 0 {
		if number > 0 {
			val++
		}
	}
	return newNumberFormulaArg(val * significance)
}

// COMBIN function calculates the number of combinations (in any order) of a
// given number objects from a set. The syntax of the function is:
//
//	COMBIN(number,number_chosen)
func (fn *formulaFuncs) COMBIN(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "COMBIN requires 2 argument")
	}
	number, chosen, val := 0.0, 0.0, 1.0
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	number = n.Number
	c := argsList.Back().Value.(formulaArg).ToNumber()
	if c.Type == ArgError {
		return c
	}
	chosen = c.Number
	number, chosen = math.Trunc(number), math.Trunc(chosen)
	if chosen > number {
		return newErrorFormulaArg(formulaErrorVALUE, "COMBIN requires number >= number_chosen")
	}
	if chosen == number || chosen == 0 {
		return newNumberFormulaArg(1)
	}
	for c := float64(1); c <= chosen; c++ {
		val *= (number + 1 - c) / c
	}
	return newNumberFormulaArg(math.Ceil(val))
}

// COMBINA function calculates the number of combinations, with repetitions,
// of a given number objects from a set. The syntax of the function is:
//
//	COMBINA(number,number_chosen)
func (fn *formulaFuncs) COMBINA(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "COMBINA requires 2 argument")
	}
	var number, chosen float64
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	number = n.Number
	c := argsList.Back().Value.(formulaArg).ToNumber()
	if c.Type == ArgError {
		return c
	}
	chosen = c.Number
	number, chosen = math.Trunc(number), math.Trunc(chosen)
	if number < chosen {
		return newErrorFormulaArg(formulaErrorVALUE, "COMBINA requires number > number_chosen")
	}
	if number == 0 {
		return newNumberFormulaArg(number)
	}
	args := list.New()
	args.PushBack(formulaArg{
		String: fmt.Sprintf("%g", number+chosen-1),
		Type:   ArgString,
	})
	args.PushBack(formulaArg{
		String: fmt.Sprintf("%g", number-1),
		Type:   ArgString,
	})
	return fn.COMBIN(args)
}

// COS function calculates the cosine of a given angle. The syntax of the
// function is:
//
//	COS(number)
func (fn *formulaFuncs) COS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COS requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	return newNumberFormulaArg(math.Cos(val.Number))
}

// COSH function calculates the hyperbolic cosine (cosh) of a supplied number.
// The syntax of the function is:
//
//	COSH(number)
func (fn *formulaFuncs) COSH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COSH requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	return newNumberFormulaArg(math.Cosh(val.Number))
}

// COT function calculates the cotangent of a given angle. The syntax of the
// function is:
//
//	COT(number)
func (fn *formulaFuncs) COT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COT requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(1 / math.Tan(val.Number))
}

// COTH function calculates the hyperbolic cotangent (coth) of a supplied
// angle. The syntax of the function is:
//
//	COTH(number)
func (fn *formulaFuncs) COTH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COTH requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg((math.Exp(val.Number) + math.Exp(-val.Number)) / (math.Exp(val.Number) - math.Exp(-val.Number)))
}

// CSC function calculates the cosecant of a given angle. The syntax of the
// function is:
//
//	CSC(number)
func (fn *formulaFuncs) CSC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "CSC requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(1 / math.Sin(val.Number))
}

// CSCH function calculates the hyperbolic cosecant (csch) of a supplied
// angle. The syntax of the function is:
//
//	CSCH(number)
func (fn *formulaFuncs) CSCH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "CSCH requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(1 / math.Sinh(val.Number))
}

// DECIMAL function converts a text representation of a number in a specified
// base, into a decimal value. The syntax of the function is:
//
//	DECIMAL(text,radix)
func (fn *formulaFuncs) DECIMAL(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "DECIMAL requires 2 numeric arguments")
	}
	text := argsList.Front().Value.(formulaArg).Value()
	var err error
	radix := argsList.Back().Value.(formulaArg).ToNumber()
	if radix.Type != ArgNumber {
		return radix
	}
	if len(text) > 2 && (strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X")) {
		text = text[2:]
	}
	val, err := strconv.ParseInt(text, int(radix.Number), 64)
	if err != nil {
		return newErrorFormulaArg(formulaErrorVALUE, err.Error())
	}
	return newNumberFormulaArg(float64(val))
}

// DEGREES function converts radians into degrees. The syntax of the function
// is:
//
//	DEGREES(angle)
func (fn *formulaFuncs) DEGREES(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "DEGREES requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(180.0 / math.Pi * val.Number)
}

// EVEN function rounds a supplied number away from zero (i.e. rounds a
// positive number up and a negative number down), to the next even number.
// The syntax of the function is:
//
//	EVEN(number)
func (fn *formulaFuncs) EVEN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "EVEN requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	sign := math.Signbit(number.Number)
	m, frac := math.Modf(number.Number / 2)
	val := m * 2
	if frac != 0 {
		if !sign {
			val += 2
		} else {
			val -= 2
		}
	}
	return newNumberFormulaArg(val)
}

// EXP function calculates the value of the mathematical constant e, raised to
// the power of a given number. The syntax of the function is:
//
//	EXP(number)
func (fn *formulaFuncs) EXP(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "EXP requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newStringFormulaArg(strings.ToUpper(fmt.Sprintf("%g", math.Exp(number.Number))))
}

// fact returns the factorial of a supplied number.
func fact(number float64) float64 {
	val := float64(1)
	for i := float64(2); i <= number; i++ {
		val *= i
	}
	return val
}

// FACT function returns the factorial of a supplied number. The syntax of the
// function is:
//
//	FACT(number)
func (fn *formulaFuncs) FACT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FACT requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(fact(number.Number))
}

// FACTDOUBLE function returns the double factorial of a supplied number. The
// syntax of the function is:
//
//	FACTDOUBLE(number)
func (fn *formulaFuncs) FACTDOUBLE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FACTDOUBLE requires 1 numeric argument")
	}
	val := 1.0
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	for i := math.Trunc(number.Number); i > 1; i -= 2 {
		val *= i
	}
	return newStringFormulaArg(strings.ToUpper(fmt.Sprintf("%g", val)))
}

// FLOOR function rounds a supplied number towards zero to the nearest
// multiple of a specified significance. The syntax of the function is:
//
//	FLOOR(number,significance)
func (fn *formulaFuncs) FLOOR(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "FLOOR requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	significance := argsList.Back().Value.(formulaArg).ToNumber()
	if significance.Type == ArgError {
		return significance
	}
	if significance.Number < 0 && number.Number >= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "invalid arguments to FLOOR")
	}
	val := number.Number
	val, res := math.Modf(val / significance.Number)
	if res != 0 {
		if number.Number < 0 && res < 0 {
			val--
		}
	}
	return newStringFormulaArg(strings.ToUpper(fmt.Sprintf("%g", val*significance.Number)))
}

// FLOORdotMATH function rounds a supplied number down to a supplied multiple
// of significance. The syntax of the function is:
//
//	FLOOR.MATH(number,[significance],[mode])
func (fn *formulaFuncs) FLOORdotMATH(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "FLOOR.MATH requires at least 1 argument")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "FLOOR.MATH allows at most 3 arguments")
	}
	significance, mode := 1.0, 1.0
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number < 0 {
		significance = -1
	}
	if argsList.Len() > 1 {
		s := argsList.Front().Next().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Floor(number.Number))
	}
	if argsList.Len() > 2 {
		m := argsList.Back().Value.(formulaArg).ToNumber()
		if m.Type == ArgError {
			return m
		}
		mode = m.Number
	}
	val, res := math.Modf(number.Number / significance)
	if res != 0 && number.Number < 0 && mode > 0 {
		val--
	}
	return newNumberFormulaArg(val * significance)
}

// FLOORdotPRECISE function rounds a supplied number down to a supplied
// multiple of significance. The syntax of the function is:
//
//	FLOOR.PRECISE(number,[significance])
func (fn *formulaFuncs) FLOORdotPRECISE(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "FLOOR.PRECISE requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "FLOOR.PRECISE allows at most 2 arguments")
	}
	var significance float64
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number < 0 {
		significance = -1
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Floor(number.Number))
	}
	if argsList.Len() > 1 {
		s := argsList.Back().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
		significance = math.Abs(significance)
		if significance == 0 {
			return newNumberFormulaArg(significance)
		}
	}
	val, res := math.Modf(number.Number / significance)
	if res != 0 {
		if number.Number < 0 {
			val--
		}
	}
	return newNumberFormulaArg(val * significance)
}

// gcd returns the greatest common divisor of two supplied integers.
func gcd(x, y float64) float64 {
	x, y = math.Trunc(x), math.Trunc(y)
	if x == 0 {
		return y
	}
	if y == 0 {
		return x
	}
	for x != y {
		if x > y {
			x = x - y
		} else {
			y = y - x
		}
	}
	return x
}

// GCD function returns the greatest common divisor of two or more supplied
// integers. The syntax of the function is:
//
//	GCD(number1,[number2],...)
func (fn *formulaFuncs) GCD(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "GCD requires at least 1 argument")
	}
	var (
		val  float64
		nums []float64
	)
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			num := token.ToNumber()
			if num.Type == ArgError {
				return num
			}
			val = num.Number
		case ArgNumber:
			val = token.Number
		}
		nums = append(nums, val)
	}
	if nums[0] < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "GCD only accepts positive arguments")
	}
	if len(nums) == 1 {
		return newNumberFormulaArg(nums[0])
	}
	cd := nums[0]
	for i := 1; i < len(nums); i++ {
		if nums[i] < 0 {
			return newErrorFormulaArg(formulaErrorVALUE, "GCD only accepts positive arguments")
		}
		cd = gcd(cd, nums[i])
	}
	return newNumberFormulaArg(cd)
}

// INT function truncates a supplied number down to the closest integer. The
// syntax of the function is:
//
//	INT(number)
func (fn *formulaFuncs) INT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "INT requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	val, frac := math.Modf(number.Number)
	if frac < 0 {
		val--
	}
	return newNumberFormulaArg(val)
}

// ISOdotCEILING function rounds a supplied number up (regardless of the
// number's sign), to the nearest multiple of a supplied significance. The
// syntax of the function is:
//
//	ISO.CEILING(number,[significance])
func (fn *formulaFuncs) ISOdotCEILING(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISO.CEILING requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISO.CEILING allows at most 2 arguments")
	}
	var significance float64
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number < 0 {
		significance = -1
	}
	if argsList.Len() == 1 {
		return newNumberFormulaArg(math.Ceil(number.Number))
	}
	if argsList.Len() > 1 {
		s := argsList.Back().Value.(formulaArg).ToNumber()
		if s.Type == ArgError {
			return s
		}
		significance = s.Number
		significance = math.Abs(significance)
		if significance == 0 {
			return newNumberFormulaArg(significance)
		}
	}
	val, res := math.Modf(number.Number / significance)
	if res != 0 {
		if number.Number > 0 {
			val++
		}
	}
	return newNumberFormulaArg(val * significance)
}

// lcm returns the least common multiple of two supplied integers.
func lcm(a, b float64) float64 {
	a = math.Trunc(a)
	b = math.Trunc(b)
	if a == 0 && b == 0 {
		return 0
	}
	return a * b / gcd(a, b)
}

// LCM function returns the least common multiple of two or more supplied
// integers. The syntax of the function is:
//
//	LCM(number1,[number2],...)
func (fn *formulaFuncs) LCM(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "LCM requires at least 1 argument")
	}
	var (
		val  float64
		nums []float64
		err  error
	)
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			if token.String == "" {
				continue
			}
			if val, err = strconv.ParseFloat(token.String, 64); err != nil {
				return newErrorFormulaArg(formulaErrorVALUE, err.Error())
			}
		case ArgNumber:
			val = token.Number
		}
		nums = append(nums, val)
	}
	if nums[0] < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "LCM only accepts positive arguments")
	}
	if len(nums) == 1 {
		return newNumberFormulaArg(nums[0])
	}
	cm := nums[0]
	for i := 1; i < len(nums); i++ {
		if nums[i] < 0 {
			return newErrorFormulaArg(formulaErrorVALUE, "LCM only accepts positive arguments")
		}
		cm = lcm(cm, nums[i])
	}
	return newNumberFormulaArg(cm)
}

// LN function calculates the natural logarithm of a given number. The syntax
// of the function is:
//
//	LN(number)
func (fn *formulaFuncs) LN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "LN requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Log(number.Number))
}

// LOG function calculates the logarithm of a given number, to a supplied
// base. The syntax of the function is:
//
//	LOG(number,[base])
func (fn *formulaFuncs) LOG(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOG requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOG allows at most 2 arguments")
	}
	base := 10.0
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if argsList.Len() > 1 {
		b := argsList.Back().Value.(formulaArg).ToNumber()
		if b.Type == ArgError {
			return b
		}
		base = b.Number
	}
	if number.Number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorDIV)
	}
	if base == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorDIV)
	}
	if base == 1 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(math.Log(number.Number) / math.Log(base))
}

// LOG10 function calculates the base 10 logarithm of a given number. The
// syntax of the function is:
//
//	LOG10(number)
func (fn *formulaFuncs) LOG10(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOG10 requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Log10(number.Number))
}

// minor function implement a minor of a matrix A is the determinant of some
// smaller square matrix.
func minor(sqMtx [][]float64, idx int) [][]float64 {
	var ret [][]float64
	for i := range sqMtx {
		if i == 0 {
			continue
		}
		var row []float64
		for j := range sqMtx {
			if j == idx {
				continue
			}
			row = append(row, sqMtx[i][j])
		}
		ret = append(ret, row)
	}
	return ret
}

// det determinant of the 2x2 matrix.
func det(sqMtx [][]float64) float64 {
	if len(sqMtx) == 2 {
		m00 := sqMtx[0][0]
		m01 := sqMtx[0][1]
		m10 := sqMtx[1][0]
		m11 := sqMtx[1][1]
		return m00*m11 - m10*m01
	}
	var res, sgn float64 = 0, 1
	for j := range sqMtx {
		res += sgn * sqMtx[0][j] * det(minor(sqMtx, j))
		sgn *= -1
	}
	return res
}

// newNumberMatrix converts a formula arguments matrix to a number matrix.
func newNumberMatrix(arg formulaArg, phalanx bool) (numMtx [][]float64, ele formulaArg) {
	rows := len(arg.Matrix)
	for r, row := range arg.Matrix {
		if phalanx && len(row) != rows {
			ele = newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
			return
		}
		numMtx = append(numMtx, make([]float64, len(row)))
		for c, cell := range row {
			if cell.Type != ArgNumber {
				ele = newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
				return
			}
			numMtx[r][c] = cell.Number
		}
	}
	return
}

// newFormulaArgMatrix converts the number formula arguments matrix to a
// formula arguments matrix.
func newFormulaArgMatrix(numMtx [][]float64) (arg [][]formulaArg) {
	for r, row := range numMtx {
		arg = append(arg, make([]formulaArg, len(row)))
		for c, cell := range row {
			arg[r][c] = newNumberFormulaArg(cell)
		}
	}
	return
}

// MDETERM calculates the determinant of a square matrix. The
// syntax of the function is:
//
//	MDETERM(array)
func (fn *formulaFuncs) MDETERM(argsList *list.List) (result formulaArg) {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MDETERM requires 1 argument")
	}
	numMtx, errArg := newNumberMatrix(argsList.Front().Value.(formulaArg), true)
	if errArg.Type == ArgError {
		return errArg
	}
	return newNumberFormulaArg(det(numMtx))
}

// cofactorMatrix returns the matrix A of cofactors.
func cofactorMatrix(i, j int, A [][]float64) float64 {
	N, sign := len(A), -1.0
	if (i+j)%2 == 0 {
		sign = 1
	}
	var B [][]float64
	B = append(B, A...)
	for m := 0; m < N; m++ {
		for n := j + 1; n < N; n++ {
			B[m][n-1] = B[m][n]
		}
		B[m] = B[m][:len(B[m])-1]
	}
	for k := i + 1; k < N; k++ {
		B[k-1] = B[k]
	}
	B = B[:len(B)-1]
	return sign * det(B)
}

// adjugateMatrix returns transpose of the cofactor matrix A with Cramer's
// rule.
func adjugateMatrix(A [][]float64) (adjA [][]float64) {
	N := len(A)
	var B [][]float64
	for i := 0; i < N; i++ {
		adjA = append(adjA, make([]float64, N))
		for j := 0; j < N; j++ {
			for m := 0; m < N; m++ {
				for n := 0; n < N; n++ {
					for x := len(B); x <= m; x++ {
						B = append(B, []float64{})
					}
					for k := len(B[m]); k <= n; k++ {
						B[m] = append(B[m], 0)
					}
					B[m][n] = A[m][n]
				}
			}
			adjA[i][j] = cofactorMatrix(j, i, B)
		}
	}
	return
}

// MINVERSE function calculates the inverse of a square matrix. The syntax of
// the function is:
//
//	MINVERSE(array)
func (fn *formulaFuncs) MINVERSE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MINVERSE requires 1 argument")
	}
	numMtx, errArg := newNumberMatrix(argsList.Front().Value.(formulaArg), true)
	if errArg.Type == ArgError {
		return errArg
	}
	if detM := det(numMtx); detM != 0 {
		datM, invertM := 1/detM, adjugateMatrix(numMtx)
		for i := 0; i < len(invertM); i++ {
			for j := 0; j < len(invertM[i]); j++ {
				invertM[i][j] *= datM
			}
		}
		return newMatrixFormulaArg(newFormulaArgMatrix(invertM))
	}
	return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
}

// MMULT function calculates the matrix product of two arrays
// (representing matrices). The syntax of the function is:
//
//	MMULT(array1,array2)
func (fn *formulaFuncs) MMULT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "MMULT requires 2 argument")
	}
	arr1 := argsList.Front().Value.(formulaArg)
	arr2 := argsList.Back().Value.(formulaArg)
	if arr1.Type == ArgNumber && arr2.Type == ArgNumber {
		return newNumberFormulaArg(arr1.Number * arr2.Number)
	}
	numMtx1, errArg1 := newNumberMatrix(arr1, false)
	if errArg1.Type == ArgError {
		return errArg1
	}
	numMtx2, errArg2 := newNumberMatrix(arr2, false)
	if errArg2.Type == ArgError {
		return errArg2
	}
	array2Rows, array2Cols := len(numMtx2), len(numMtx2[0])
	if len(numMtx1[0]) != array2Rows {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	var numMtx [][]float64
	var row1, row []float64
	var sum float64
	for i := 0; i < len(numMtx1); i++ {
		numMtx = append(numMtx, []float64{})
		row = []float64{}
		row1 = numMtx1[i]
		for j := 0; j < array2Cols; j++ {
			sum = 0
			for k := 0; k < array2Rows; k++ {
				sum += row1[k] * numMtx2[k][j]
			}
			for l := len(row); l <= j; l++ {
				row = append(row, 0)
			}
			row[j] = sum
			numMtx[i] = row
		}
	}
	return newMatrixFormulaArg(newFormulaArgMatrix(numMtx))
}

// MOD function returns the remainder of a division between two supplied
// numbers. The syntax of the function is:
//
//	MOD(number,divisor)
func (fn *formulaFuncs) MOD(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "MOD requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	divisor := argsList.Back().Value.(formulaArg).ToNumber()
	if divisor.Type == ArgError {
		return divisor
	}
	if divisor.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, "MOD divide by zero")
	}
	trunc, rem := math.Modf(number.Number / divisor.Number)
	if rem < 0 {
		trunc--
	}
	return newNumberFormulaArg(number.Number - divisor.Number*trunc)
}

// MROUND function rounds a supplied number up or down to the nearest multiple
// of a given number. The syntax of the function is:
//
//	MROUND(number,multiple)
func (fn *formulaFuncs) MROUND(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "MROUND requires 2 numeric arguments")
	}
	n := argsList.Front().Value.(formulaArg).ToNumber()
	if n.Type == ArgError {
		return n
	}
	multiple := argsList.Back().Value.(formulaArg).ToNumber()
	if multiple.Type == ArgError {
		return multiple
	}
	if multiple.Number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if multiple.Number < 0 && n.Number > 0 ||
		multiple.Number > 0 && n.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	number, res := math.Modf(n.Number / multiple.Number)
	if math.Trunc(res+0.5) > 0 {
		number++
	}
	return newNumberFormulaArg(number * multiple.Number)
}

// MULTINOMIAL function calculates the ratio of the factorial of a sum of
// supplied values to the product of factorials of those values. The syntax of
// the function is:
//
//	MULTINOMIAL(number1,[number2],...)
func (fn *formulaFuncs) MULTINOMIAL(argsList *list.List) formulaArg {
	val, num, denom := 0.0, 0.0, 1.0
	var err error
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			if token.String == "" {
				continue
			}
			if val, err = strconv.ParseFloat(token.String, 64); err != nil {
				return newErrorFormulaArg(formulaErrorVALUE, err.Error())
			}
		case ArgNumber:
			val = token.Number
		}
		num += val
		denom *= fact(val)
	}
	return newNumberFormulaArg(fact(num) / denom)
}

// MUNIT function returns the unit matrix for a specified dimension. The
// syntax of the function is:
//
//	MUNIT(dimension)
func (fn *formulaFuncs) MUNIT(argsList *list.List) (result formulaArg) {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MUNIT requires 1 numeric argument")
	}
	dimension := argsList.Back().Value.(formulaArg).ToNumber()
	if dimension.Type == ArgError || dimension.Number < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, dimension.Error)
	}
	matrix := make([][]formulaArg, 0, int(dimension.Number))
	for i := 0; i < int(dimension.Number); i++ {
		row := make([]formulaArg, int(dimension.Number))
		for j := 0; j < int(dimension.Number); j++ {
			if i == j {
				row[j] = newNumberFormulaArg(1.0)
			} else {
				row[j] = newNumberFormulaArg(0.0)
			}
		}
		matrix = append(matrix, row)
	}
	return newMatrixFormulaArg(matrix)
}

// ODD function ounds a supplied number away from zero (i.e. rounds a positive
// number up and a negative number down), to the next odd number. The syntax
// of the function is:
//
//	ODD(number)
func (fn *formulaFuncs) ODD(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ODD requires 1 numeric argument")
	}
	number := argsList.Back().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if number.Number == 0 {
		return newNumberFormulaArg(1)
	}
	sign := math.Signbit(number.Number)
	m, frac := math.Modf((number.Number - 1) / 2)
	val := m*2 + 1
	if frac != 0 {
		if !sign {
			val += 2
		} else {
			val -= 2
		}
	}
	return newNumberFormulaArg(val)
}

// PI function returns the value of the mathematical constant π (pi), accurate
// to 15 digits (14 decimal places). The syntax of the function is:
//
//	PI()
func (fn *formulaFuncs) PI(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "PI accepts no arguments")
	}
	return newNumberFormulaArg(math.Pi)
}

// POWER function calculates a given number, raised to a supplied power.
// The syntax of the function is:
//
//	POWER(number,power)
func (fn *formulaFuncs) POWER(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "POWER requires 2 numeric arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type == ArgError {
		return x
	}
	y := argsList.Back().Value.(formulaArg).ToNumber()
	if y.Type == ArgError {
		return y
	}
	if x.Number == 0 && y.Number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if x.Number == 0 && y.Number < 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(math.Pow(x.Number, y.Number))
}

// PRODUCT function returns the product (multiplication) of a supplied set of
// numerical values. The syntax of the function is:
//
//	PRODUCT(number1,[number2],...)
func (fn *formulaFuncs) PRODUCT(argsList *list.List) formulaArg {
	product := 1.0
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			num := token.ToNumber()
			if num.Type != ArgNumber {
				return num
			}
			product = product * num.Number
		case ArgNumber:
			product = product * token.Number
		case ArgMatrix:
			for _, row := range token.Matrix {
				for _, cell := range row {
					if cell.Type == ArgNumber {
						product *= cell.Number
					}
				}
			}
		}
	}
	return newNumberFormulaArg(product)
}

// QUOTIENT function returns the integer portion of a division between two
// supplied numbers. The syntax of the function is:
//
//	QUOTIENT(numerator,denominator)
func (fn *formulaFuncs) QUOTIENT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "QUOTIENT requires 2 numeric arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type == ArgError {
		return x
	}
	y := argsList.Back().Value.(formulaArg).ToNumber()
	if y.Type == ArgError {
		return y
	}
	if y.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(math.Trunc(x.Number / y.Number))
}

// RADIANS function converts radians into degrees. The syntax of the function is:
//
//	RADIANS(angle)
func (fn *formulaFuncs) RADIANS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "RADIANS requires 1 numeric argument")
	}
	angle := argsList.Front().Value.(formulaArg).ToNumber()
	if angle.Type == ArgError {
		return angle
	}
	return newNumberFormulaArg(math.Pi / 180.0 * angle.Number)
}

// RAND function generates a random real number between 0 and 1. The syntax of
// the function is:
//
//	RAND()
func (fn *formulaFuncs) RAND(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "RAND accepts no arguments")
	}
	return newNumberFormulaArg(rand.New(rand.NewSource(time.Now().UnixNano())).Float64())
}

// RANDBETWEEN function generates a random integer between two supplied
// integers. The syntax of the function is:
//
//	RANDBETWEEN(bottom,top)
func (fn *formulaFuncs) RANDBETWEEN(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "RANDBETWEEN requires 2 numeric arguments")
	}
	bottom := argsList.Front().Value.(formulaArg).ToNumber()
	if bottom.Type == ArgError {
		return bottom
	}
	top := argsList.Back().Value.(formulaArg).ToNumber()
	if top.Type == ArgError {
		return top
	}
	if top.Number < bottom.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	num := rand.New(rand.NewSource(time.Now().UnixNano())).Int63n(int64(top.Number - bottom.Number + 1))
	return newNumberFormulaArg(float64(num + int64(bottom.Number)))
}

// romanNumerals defined a numeral system that originated in ancient Rome and
// remained the usual way of writing numbers throughout Europe well into the
// Late Middle Ages.
type romanNumerals struct {
	n float64
	s string
}

var romanTable = [][]romanNumerals{
	{
		{1000, "M"},
		{900, "CM"},
		{500, "D"},
		{400, "CD"},
		{100, "C"},
		{90, "XC"},
		{50, "L"},
		{40, "XL"},
		{10, "X"},
		{9, "IX"},
		{5, "V"},
		{4, "IV"},
		{1, "I"},
	},
	{
		{1000, "M"},
		{950, "LM"},
		{900, "CM"},
		{500, "D"},
		{450, "LD"},
		{400, "CD"},
		{100, "C"},
		{95, "VC"},
		{90, "XC"},
		{50, "L"},
		{45, "VL"},
		{40, "XL"},
		{10, "X"},
		{9, "IX"},
		{5, "V"},
		{4, "IV"},
		{1, "I"},
	},
	{
		{1000, "M"},
		{990, "XM"},
		{950, "LM"},
		{900, "CM"},
		{500, "D"},
		{490, "XD"},
		{450, "LD"},
		{400, "CD"},
		{100, "C"},
		{99, "IC"},
		{90, "XC"},
		{50, "L"},
		{45, "VL"},
		{40, "XL"},
		{10, "X"},
		{9, "IX"},
		{5, "V"},
		{4, "IV"},
		{1, "I"},
	},
	{
		{1000, "M"},
		{995, "VM"},
		{990, "XM"},
		{950, "LM"},
		{900, "CM"},
		{500, "D"},
		{495, "VD"},
		{490, "XD"},
		{450, "LD"},
		{400, "CD"},
		{100, "C"},
		{99, "IC"},
		{90, "XC"},
		{50, "L"},
		{45, "VL"},
		{40, "XL"},
		{10, "X"},
		{9, "IX"},
		{5, "V"},
		{4, "IV"},
		{1, "I"},
	},
	{
		{1000, "M"},
		{999, "IM"},
		{995, "VM"},
		{990, "XM"},
		{950, "LM"},
		{900, "CM"},
		{500, "D"},
		{499, "ID"},
		{495, "VD"},
		{490, "XD"},
		{450, "LD"},
		{400, "CD"},
		{100, "C"},
		{99, "IC"},
		{90, "XC"},
		{50, "L"},
		{45, "VL"},
		{40, "XL"},
		{10, "X"},
		{9, "IX"},
		{5, "V"},
		{4, "IV"},
		{1, "I"},
	},
}

// ROMAN function converts an arabic number to Roman. I.e. for a supplied
// integer, the function returns a text string depicting the roman numeral
// form of the number. The syntax of the function is:
//
//	ROMAN(number,[form])
func (fn *formulaFuncs) ROMAN(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROMAN requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROMAN allows at most 2 arguments")
	}
	var form int
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if argsList.Len() > 1 {
		f := argsList.Back().Value.(formulaArg).ToNumber()
		if f.Type == ArgError {
			return f
		}
		form = int(f.Number)
		if form < 0 {
			form = 0
		} else if form > 4 {
			form = 4
		}
	}
	decimalTable := romanTable[0]
	switch form {
	case 1:
		decimalTable = romanTable[1]
	case 2:
		decimalTable = romanTable[2]
	case 3:
		decimalTable = romanTable[3]
	case 4:
		decimalTable = romanTable[4]
	}
	val := math.Trunc(number.Number)
	buf := bytes.Buffer{}
	for _, r := range decimalTable {
		for val >= r.n {
			buf.WriteString(r.s)
			val -= r.n
		}
	}
	return newStringFormulaArg(buf.String())
}

type roundMode byte

const (
	closest roundMode = iota
	down
	up
)

// round rounds a supplied number up or down.
func (fn *formulaFuncs) round(number, digits float64, mode roundMode) float64 {
	var significance float64
	if digits > 0 {
		significance = math.Pow(1/10.0, digits)
	} else {
		significance = math.Pow(10.0, -digits)
	}
	val, res := math.Modf(number / significance)
	switch mode {
	case closest:
		const eps = 0.499999999
		if res >= eps {
			val++
		} else if res <= -eps {
			val--
		}
	case down:
	case up:
		if res > 0 {
			val++
		} else if res < 0 {
			val--
		}
	}
	return val * significance
}

// ROUND function rounds a supplied number up or down, to a specified number
// of decimal places. The syntax of the function is:
//
//	ROUND(number,num_digits)
func (fn *formulaFuncs) ROUND(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROUND requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	digits := argsList.Back().Value.(formulaArg).ToNumber()
	if digits.Type == ArgError {
		return digits
	}
	return newNumberFormulaArg(fn.round(number.Number, digits.Number, closest))
}

// ROUNDDOWN function rounds a supplied number down towards zero, to a
// specified number of decimal places. The syntax of the function is:
//
//	ROUNDDOWN(number,num_digits)
func (fn *formulaFuncs) ROUNDDOWN(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROUNDDOWN requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	digits := argsList.Back().Value.(formulaArg).ToNumber()
	if digits.Type == ArgError {
		return digits
	}
	return newNumberFormulaArg(fn.round(number.Number, digits.Number, down))
}

// ROUNDUP function rounds a supplied number up, away from zero, to a
// specified number of decimal places. The syntax of the function is:
//
//	ROUNDUP(number,num_digits)
func (fn *formulaFuncs) ROUNDUP(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROUNDUP requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	digits := argsList.Back().Value.(formulaArg).ToNumber()
	if digits.Type == ArgError {
		return digits
	}
	return newNumberFormulaArg(fn.round(number.Number, digits.Number, up))
}

// SEC function calculates the secant of a given angle. The syntax of the
// function is:
//
//	SEC(number)
func (fn *formulaFuncs) SEC(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SEC requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Cos(number.Number))
}

// SECH function calculates the hyperbolic secant (sech) of a supplied angle.
// The syntax of the function is:
//
//	SECH(number)
func (fn *formulaFuncs) SECH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SECH requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(1 / math.Cosh(number.Number))
}

// SERIESSUM function returns the sum of a power series. The syntax of the
// function is:
//
//	SERIESSUM(x,n,m,coefficients)
func (fn *formulaFuncs) SERIESSUM(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "SERIESSUM requires 4 arguments")
	}
	var x, n, m formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if n = argsList.Front().Next().Value.(formulaArg).ToNumber(); n.Type != ArgNumber {
		return n
	}
	if m = argsList.Front().Next().Next().Value.(formulaArg).ToNumber(); m.Type != ArgNumber {
		return m
	}
	var result, i float64
	for _, coefficient := range argsList.Back().Value.(formulaArg).ToList() {
		if coefficient.Value() == "" {
			continue
		}
		num := coefficient.ToNumber()
		if num.Type != ArgNumber {
			return num
		}
		result += num.Number * math.Pow(x.Number, n.Number+(m.Number*i))
		i++
	}
	return newNumberFormulaArg(result)
}

// SIGN function returns the arithmetic sign (+1, -1 or 0) of a supplied
// number. I.e. if the number is positive, the Sign function returns +1, if
// the number is negative, the function returns -1 and if the number is 0
// (zero), the function returns 0. The syntax of the function is:
//
//	SIGN(number)
func (fn *formulaFuncs) SIGN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SIGN requires 1 numeric argument")
	}
	val := argsList.Front().Value.(formulaArg).ToNumber()
	if val.Type == ArgError {
		return val
	}
	if val.Number < 0 {
		return newNumberFormulaArg(-1)
	}
	if val.Number > 0 {
		return newNumberFormulaArg(1)
	}
	return newNumberFormulaArg(0)
}

// SIN function calculates the sine of a given angle. The syntax of the
// function is:
//
//	SIN(number)
func (fn *formulaFuncs) SIN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SIN requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Sin(number.Number))
}

// SINH function calculates the hyperbolic sine (sinh) of a supplied number.
// The syntax of the function is:
//
//	SINH(number)
func (fn *formulaFuncs) SINH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SINH requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Sinh(number.Number))
}

// SQRT function calculates the positive square root of a supplied number. The
// syntax of the function is:
//
//	SQRT(number)
func (fn *formulaFuncs) SQRT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SQRT requires 1 numeric argument")
	}
	value := argsList.Front().Value.(formulaArg).ToNumber()
	if value.Type == ArgError {
		return value
	}
	if value.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(math.Sqrt(value.Number))
}

// SQRTPI function returns the square root of a supplied number multiplied by
// the mathematical constant, π. The syntax of the function is:
//
//	SQRTPI(number)
func (fn *formulaFuncs) SQRTPI(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SQRTPI requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Sqrt(number.Number * math.Pi))
}

// STDEV function calculates the sample standard deviation of a supplied set
// of values. The syntax of the function is:
//
//	STDEV(number1,[number2],...)
func (fn *formulaFuncs) STDEV(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "STDEV requires at least 1 argument")
	}
	return fn.stdev(false, argsList)
}

// STDEVdotS function calculates the sample standard deviation of a supplied
// set of values. The syntax of the function is:
//
//	STDEV.S(number1,[number2],...)
func (fn *formulaFuncs) STDEVdotS(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "STDEV.S requires at least 1 argument")
	}
	return fn.stdev(false, argsList)
}

// STDEVA function estimates standard deviation based on a sample. The
// standard deviation is a measure of how widely values are dispersed from
// the average value (the mean). The syntax of the function is:
//
//	STDEVA(number1,[number2],...)
func (fn *formulaFuncs) STDEVA(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "STDEVA requires at least 1 argument")
	}
	return fn.stdev(true, argsList)
}

// calcStdevPow is part of the implementation stdev.
func calcStdevPow(result, count float64, n, m formulaArg) (float64, float64) {
	if result == -1 {
		result = math.Pow(n.Number-m.Number, 2)
	} else {
		result += math.Pow(n.Number-m.Number, 2)
	}
	count++
	return result, count
}

// calcStdev is part of the implementation stdev.
func calcStdev(stdeva bool, result, count float64, mean, token formulaArg) (float64, float64) {
	for _, row := range token.ToList() {
		if row.Type == ArgNumber || row.Type == ArgString {
			if !stdeva && (row.Value() == "TRUE" || row.Value() == "FALSE") {
				continue
			} else if stdeva && (row.Value() == "TRUE" || row.Value() == "FALSE") {
				num := row.ToBool()
				if num.Type == ArgNumber {
					result, count = calcStdevPow(result, count, num, mean)
					continue
				}
			} else {
				num := row.ToNumber()
				if num.Type == ArgNumber {
					result, count = calcStdevPow(result, count, num, mean)
				}
			}
		}
	}
	return result, count
}

// stdev is an implementation of the formula functions STDEV and STDEVA.
func (fn *formulaFuncs) stdev(stdeva bool, argsList *list.List) formulaArg {
	count, result := -1.0, -1.0
	var mean formulaArg
	if stdeva {
		mean = fn.AVERAGEA(argsList)
	} else {
		mean = fn.AVERAGE(argsList)
	}
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString, ArgNumber:
			if !stdeva && (token.Value() == "TRUE" || token.Value() == "FALSE") {
				continue
			} else if stdeva && (token.Value() == "TRUE" || token.Value() == "FALSE") {
				num := token.ToBool()
				if num.Type == ArgNumber {
					result, count = calcStdevPow(result, count, num, mean)
					continue
				}
			} else {
				num := token.ToNumber()
				if num.Type == ArgNumber {
					result, count = calcStdevPow(result, count, num, mean)
				}
			}
		case ArgList, ArgMatrix:
			result, count = calcStdev(stdeva, result, count, mean, token)
		}
	}
	if count > 0 && result >= 0 {
		return newNumberFormulaArg(math.Sqrt(result / count))
	}
	return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
}

// POISSONdotDIST function calculates the Poisson Probability Mass Function or
// the Cumulative Poisson Probability Function for a supplied set of
// parameters. The syntax of the function is:
//
//	POISSON.DIST(x,mean,cumulative)
func (fn *formulaFuncs) POISSONdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "POISSON.DIST requires 3 arguments")
	}
	return fn.POISSON(argsList)
}

// POISSON function calculates the Poisson Probability Mass Function or the
// Cumulative Poisson Probability Function for a supplied set of parameters.
// The syntax of the function is:
//
//	POISSON(x,mean,cumulative)
func (fn *formulaFuncs) POISSON(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "POISSON requires 3 arguments")
	}
	var x, mean, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if x.Number < 0 || mean.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if cumulative.Number == 1 {
		summer := 0.0
		floor := math.Floor(x.Number)
		for i := 0; i <= int(floor); i++ {
			summer += math.Pow(mean.Number, float64(i)) / fact(float64(i))
		}
		return newNumberFormulaArg(math.Exp(0-mean.Number) * summer)
	}
	return newNumberFormulaArg(math.Exp(0-mean.Number) * math.Pow(mean.Number, x.Number) / fact(x.Number))
}

// prepareProbArgs checking and prepare arguments for the formula function
// PROB.
func prepareProbArgs(argsList *list.List) []formulaArg {
	if argsList.Len() < 3 {
		return []formulaArg{newErrorFormulaArg(formulaErrorVALUE, "PROB requires at least 3 arguments")}
	}
	if argsList.Len() > 4 {
		return []formulaArg{newErrorFormulaArg(formulaErrorVALUE, "PROB requires at most 4 arguments")}
	}
	var lower, upper formulaArg
	xRange := argsList.Front().Value.(formulaArg)
	probRange := argsList.Front().Next().Value.(formulaArg)
	if lower = argsList.Front().Next().Next().Value.(formulaArg); lower.Type != ArgNumber {
		return []formulaArg{newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)}
	}
	upper = lower
	if argsList.Len() == 4 {
		if upper = argsList.Back().Value.(formulaArg); upper.Type != ArgNumber {
			return []formulaArg{newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)}
		}
	}
	nR1, nR2 := len(xRange.Matrix), len(probRange.Matrix)
	if nR1 == 0 || nR2 == 0 {
		return []formulaArg{newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)}
	}
	if nR1 != nR2 {
		return []formulaArg{newErrorFormulaArg(formulaErrorNA, formulaErrorNA)}
	}
	nC1, nC2 := len(xRange.Matrix[0]), len(probRange.Matrix[0])
	if nC1 != nC2 {
		return []formulaArg{newErrorFormulaArg(formulaErrorNA, formulaErrorNA)}
	}
	return []formulaArg{xRange, probRange, lower, upper}
}

// PROB function calculates the probability associated with a given range. The
// syntax of the function is:
//
//	PROB(x_range,prob_range,lower_limit,[upper_limit])
func (fn *formulaFuncs) PROB(argsList *list.List) formulaArg {
	args := prepareProbArgs(argsList)
	if len(args) == 1 {
		return args[0]
	}
	xRange, probRange, lower, upper := args[0], args[1], args[2], args[3]
	var sum, res, fP, fW float64
	var stop bool
	for r := 0; r < len(xRange.Matrix) && !stop; r++ {
		for c := 0; c < len(xRange.Matrix[0]) && !stop; c++ {
			p := probRange.Matrix[r][c]
			x := xRange.Matrix[r][c]
			if p.Type == ArgNumber && x.Type == ArgNumber {
				if fP, fW = p.Number, x.Number; fP < 0 || fP > 1 {
					stop = true
					continue
				}
				if sum += fP; fW >= lower.Number && fW <= upper.Number {
					res += fP
				}
				continue
			}
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	if stop || math.Abs(sum-1) > 1.0e-7 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(res)
}

// SUBTOTAL function performs a specified calculation (e.g. the sum, product,
// average, etc.) for a supplied set of values. The syntax of the function is:
//
//	SUBTOTAL(function_num,ref1,[ref2],...)
func (fn *formulaFuncs) SUBTOTAL(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "SUBTOTAL requires at least 2 arguments")
	}
	var fnNum formulaArg
	if fnNum = argsList.Front().Value.(formulaArg).ToNumber(); fnNum.Type != ArgNumber {
		return fnNum
	}
	subFn, ok := map[int]func(argsList *list.List) formulaArg{
		1: fn.AVERAGE, 101: fn.AVERAGE,
		2: fn.COUNT, 102: fn.COUNT,
		3: fn.COUNTA, 103: fn.COUNTA,
		4: fn.MAX, 104: fn.MAX,
		5: fn.MIN, 105: fn.MIN,
		6: fn.PRODUCT, 106: fn.PRODUCT,
		7: fn.STDEV, 107: fn.STDEV,
		8: fn.STDEVP, 108: fn.STDEVP,
		9: fn.SUM, 109: fn.SUM,
		10: fn.VAR, 110: fn.VAR,
		11: fn.VARP, 111: fn.VARP,
	}[int(fnNum.Number)]
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, "SUBTOTAL has invalid function_num")
	}
	subArgList := list.New().Init()
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		subArgList.PushBack(arg.Value.(formulaArg))
	}
	return subFn(subArgList)
}

// SUM function adds together a supplied set of numbers and returns the sum of
// these values. The syntax of the function is:
//
//	SUM(number1,[number2],...)
func (fn *formulaFuncs) SUM(argsList *list.List) formulaArg {
	var sum float64
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgError:
			return token
		case ArgString:
			if num := token.ToNumber(); num.Type == ArgNumber {
				sum += num.Number
			}
		case ArgNumber:
			sum += token.Number
		case ArgMatrix:
			for _, row := range token.Matrix {
				for _, value := range row {
					if num := value.ToNumber(); num.Type == ArgNumber {
						sum += num.Number
					}
				}
			}
		}
	}
	return newNumberFormulaArg(sum)
}

// SUMIF function finds the values in a supplied array, that satisfy a given
// criteria, and returns the sum of the corresponding values in a second
// supplied array. The syntax of the function is:
//
//	SUMIF(range,criteria,[sum_range])
func (fn *formulaFuncs) SUMIF(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "SUMIF requires at least 2 arguments")
	}
	criteria := formulaCriteriaParser(argsList.Front().Next().Value.(formulaArg))
	rangeMtx := argsList.Front().Value.(formulaArg).Matrix
	var sumRange [][]formulaArg
	if argsList.Len() == 3 {
		sumRange = argsList.Back().Value.(formulaArg).Matrix
	}
	var sum float64
	var arg formulaArg
	for rowIdx, row := range rangeMtx {
		for colIdx, cell := range row {
			arg = cell
			if arg.Type == ArgEmpty {
				continue
			}
			if ok, _ := formulaCriteriaEval(arg, criteria); ok {
				if argsList.Len() == 3 {
					if len(sumRange) > rowIdx && len(sumRange[rowIdx]) > colIdx {
						arg = sumRange[rowIdx][colIdx]
					}
				}
				if arg.Type == ArgNumber {
					sum += arg.Number
				}
			}
		}
	}
	return newNumberFormulaArg(sum)
}

// SUMIFS function finds values in one or more supplied arrays, that satisfy a
// set of criteria, and returns the sum of the corresponding values in a
// further supplied array. The syntax of the function is:
//
//	SUMIFS(sum_range,criteria_range1,criteria1,[criteria_range2,criteria2],...)
func (fn *formulaFuncs) SUMIFS(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "SUMIFS requires at least 3 arguments")
	}
	if argsList.Len()%2 != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var args []formulaArg
	sum, sumRange := 0.0, argsList.Front().Value.(formulaArg).Matrix
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	for _, ref := range formulaIfsMatch(args) {
		if ref.Row >= len(sumRange) || ref.Col >= len(sumRange[ref.Row]) {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		if num := sumRange[ref.Row][ref.Col].ToNumber(); num.Type == ArgNumber {
			sum += num.Number
		}
	}
	return newNumberFormulaArg(sum)
}

// sumproduct is an implementation of the formula function SUMPRODUCT.
func (fn *formulaFuncs) sumproduct(argsList *list.List) formulaArg {
	var (
		argType ArgType
		n       int
		res     []float64
		sum     float64
	)
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		if argType == ArgUnknown {
			argType = token.Type
		}
		if token.Type != argType {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		switch token.Type {
		case ArgString, ArgNumber:
			if num := token.ToNumber(); num.Type == ArgNumber {
				sum = fn.PRODUCT(argsList).Number
				continue
			}
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		case ArgMatrix:
			args := token.ToList()
			if res == nil {
				n = len(args)
				res = make([]float64, n)
				for i := range res {
					res[i] = 1.0
				}
			}
			if len(args) != n {
				return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
			}
			for i, value := range args {
				num := value.ToNumber()
				if num.Type != ArgNumber && value.Value() != "" {
					return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
				}
				res[i] = res[i] * num.Number
			}
		}
	}
	for _, r := range res {
		sum += r
	}
	return newNumberFormulaArg(sum)
}

// SUMPRODUCT function returns the sum of the products of the corresponding
// values in a set of supplied arrays. The syntax of the function is:
//
//	SUMPRODUCT(array1,[array2],[array3],...)
func (fn *formulaFuncs) SUMPRODUCT(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SUMPRODUCT requires at least 1 argument")
	}
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		if token := arg.Value.(formulaArg); token.Type == ArgError {
			return token
		}
	}
	return fn.sumproduct(argsList)
}

// SUMSQ function returns the sum of squares of a supplied set of values. The
// syntax of the function is:
//
//	SUMSQ(number1,[number2],...)
func (fn *formulaFuncs) SUMSQ(argsList *list.List) formulaArg {
	var val, sq float64
	var err error
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgString:
			if token.String == "" {
				continue
			}
			if val, err = strconv.ParseFloat(token.String, 64); err != nil {
				return newErrorFormulaArg(formulaErrorVALUE, err.Error())
			}
			sq += val * val
		case ArgNumber:
			sq += token.Number * token.Number
		case ArgMatrix:
			for _, row := range token.Matrix {
				for _, value := range row {
					if value.Value() == "" {
						continue
					}
					if val, err = strconv.ParseFloat(value.Value(), 64); err != nil {
						return newErrorFormulaArg(formulaErrorVALUE, err.Error())
					}
					sq += val * val
				}
			}
		}
	}
	return newNumberFormulaArg(sq)
}

// sumx is an implementation of the formula functions SUMX2MY2, SUMX2PY2 and
// SUMXMY2.
func (fn *formulaFuncs) sumx(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 arguments", name))
	}
	array1 := argsList.Front().Value.(formulaArg)
	array2 := argsList.Back().Value.(formulaArg)
	left, right := array1.ToList(), array2.ToList()
	n := len(left)
	if n != len(right) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	result := 0.0
	for i := 0; i < n; i++ {
		if lhs, rhs := left[i].ToNumber(), right[i].ToNumber(); lhs.Number != 0 && rhs.Number != 0 {
			switch name {
			case "SUMX2MY2":
				result += lhs.Number*lhs.Number - rhs.Number*rhs.Number
			case "SUMX2PY2":
				result += lhs.Number*lhs.Number + rhs.Number*rhs.Number
			default:
				result += (lhs.Number - rhs.Number) * (lhs.Number - rhs.Number)
			}
		}
	}
	return newNumberFormulaArg(result)
}

// SUMX2MY2 function returns the sum of the differences of squares of two
// supplied sets of values. The syntax of the function is:
//
//	SUMX2MY2(array_x,array_y)
func (fn *formulaFuncs) SUMX2MY2(argsList *list.List) formulaArg {
	return fn.sumx("SUMX2MY2", argsList)
}

// SUMX2PY2 function returns the sum of the sum of squares of two supplied sets
// of values. The syntax of the function is:
//
//	SUMX2PY2(array_x,array_y)
func (fn *formulaFuncs) SUMX2PY2(argsList *list.List) formulaArg {
	return fn.sumx("SUMX2PY2", argsList)
}

// SUMXMY2 function returns the sum of the squares of differences between
// corresponding values in two supplied arrays. The syntax of the function
// is:
//
//	SUMXMY2(array_x,array_y)
func (fn *formulaFuncs) SUMXMY2(argsList *list.List) formulaArg {
	return fn.sumx("SUMXMY2", argsList)
}

// TAN function calculates the tangent of a given angle. The syntax of the
// function is:
//
//	TAN(number)
func (fn *formulaFuncs) TAN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TAN requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Tan(number.Number))
}

// TANH function calculates the hyperbolic tangent (tanh) of a supplied
// number. The syntax of the function is:
//
//	TANH(number)
func (fn *formulaFuncs) TANH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TANH requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	return newNumberFormulaArg(math.Tanh(number.Number))
}

// TRUNC function truncates a supplied number to a specified number of decimal
// places. The syntax of the function is:
//
//	TRUNC(number,[number_digits])
func (fn *formulaFuncs) TRUNC(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "TRUNC requires at least 1 argument")
	}
	var digits, adjust, rtrim float64
	var err error
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type == ArgError {
		return number
	}
	if argsList.Len() > 1 {
		d := argsList.Back().Value.(formulaArg).ToNumber()
		if d.Type == ArgError {
			return d
		}
		digits = d.Number
		digits = math.Floor(digits)
	}
	adjust = math.Pow(10, digits)
	x := int((math.Abs(number.Number) - math.Abs(float64(int(number.Number)))) * adjust)
	if x != 0 {
		if rtrim, err = strconv.ParseFloat(strings.TrimRight(strconv.Itoa(x), "0"), 64); err != nil {
			return newErrorFormulaArg(formulaErrorVALUE, err.Error())
		}
	}
	if (digits > 0) && (rtrim < adjust/10) {
		return newNumberFormulaArg(number.Number)
	}
	return newNumberFormulaArg(float64(int(number.Number*adjust)) / adjust)
}

// Statistical Functions

// AVEDEV function calculates the average deviation of a supplied set of
// values. The syntax of the function is:
//
//	AVEDEV(number1,[number2],...)
func (fn *formulaFuncs) AVEDEV(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "AVEDEV requires at least 1 argument")
	}
	average := fn.AVERAGE(argsList)
	if average.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	result, count := 0.0, 0.0
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		num := arg.Value.(formulaArg).ToNumber()
		if num.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		result += math.Abs(num.Number - average.Number)
		count++
	}
	return newNumberFormulaArg(result / count)
}

// AVERAGE function returns the arithmetic mean of a list of supplied numbers.
// The syntax of the function is:
//
//	AVERAGE(number1,[number2],...)
func (fn *formulaFuncs) AVERAGE(argsList *list.List) formulaArg {
	var args []formulaArg
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	count, sum := fn.countSum(false, args)
	if count == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(sum / count)
}

// AVERAGEA function returns the arithmetic mean of a list of supplied numbers
// with text cell and zero values. The syntax of the function is:
//
//	AVERAGEA(number1,[number2],...)
func (fn *formulaFuncs) AVERAGEA(argsList *list.List) formulaArg {
	var args []formulaArg
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	count, sum := fn.countSum(true, args)
	if count == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(sum / count)
}

// AVERAGEIF function finds the values in a supplied array that satisfy a
// specified criteria, and returns the average (i.e. the statistical mean) of
// the corresponding values in a second supplied array. The syntax of the
// function is:
//
//	AVERAGEIF(range,criteria,[average_range])
func (fn *formulaFuncs) AVERAGEIF(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "AVERAGEIF requires at least 2 arguments")
	}
	var (
		criteria  = formulaCriteriaParser(argsList.Front().Next().Value.(formulaArg))
		rangeMtx  = argsList.Front().Value.(formulaArg).Matrix
		cellRange [][]formulaArg
		args      []formulaArg
		val       float64
		err       error
		ok        bool
	)
	if argsList.Len() == 3 {
		cellRange = argsList.Back().Value.(formulaArg).Matrix
	}
	for rowIdx, row := range rangeMtx {
		for colIdx, col := range row {
			fromVal := col.Value()
			if fromVal == "" {
				continue
			}
			if col.Type == ArgString && criteria.Condition.Type != ArgString {
				continue
			}
			ok, _ = formulaCriteriaEval(col, criteria)
			if ok {
				if argsList.Len() == 3 {
					if len(cellRange) > rowIdx && len(cellRange[rowIdx]) > colIdx {
						fromVal = cellRange[rowIdx][colIdx].Value()
					}
				}
				if val, err = strconv.ParseFloat(fromVal, 64); err != nil {
					continue
				}
				args = append(args, newNumberFormulaArg(val))
			}
		}
	}
	count, sum := fn.countSum(false, args)
	if count == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(sum / count)
}

// AVERAGEIFS function finds entries in one or more arrays, that satisfy a set
// of supplied criteria, and returns the average (i.e. the statistical mean)
// of the corresponding values in a further supplied array. The syntax of the
// function is:
//
//	AVERAGEIFS(average_range,criteria_range1,criteria1,[criteria_range2,criteria2],...)
func (fn *formulaFuncs) AVERAGEIFS(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "AVERAGEIFS requires at least 3 arguments")
	}
	if argsList.Len()%2 != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var args []formulaArg
	sum, sumRange := 0.0, argsList.Front().Value.(formulaArg).Matrix
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	count := 0.0
	for _, ref := range formulaIfsMatch(args) {
		if num := sumRange[ref.Row][ref.Col].ToNumber(); num.Type == ArgNumber {
			sum += num.Number
			count++
		}
	}
	if count == 0 {
		return newErrorFormulaArg(formulaErrorDIV, "AVERAGEIF divide by zero")
	}
	return newNumberFormulaArg(sum / count)
}

// getBetaHelperContFrac continued fractions for the beta function.
func getBetaHelperContFrac(fX, fA, fB float64) float64 {
	var a1, b1, a2, b2, fnorm, cfnew, cf, rm float64
	a1, b1, b2 = 1, 1, 1-(fA+fB)/(fA+1)*fX
	if b2 == 0 {
		a2, fnorm, cf = 0, 1, 1
	} else {
		a2, fnorm = 1, 1/b2
		cf = a2 * fnorm
	}
	cfnew, rm = 1, 1
	fMaxIter, fMachEps := 50000.0, 2.22045e-016
	bfinished := false
	for rm < fMaxIter && !bfinished {
		apl2m := fA + 2*rm
		d2m := rm * (fB - rm) * fX / ((apl2m - 1) * apl2m)
		d2m1 := -(fA + rm) * (fA + fB + rm) * fX / (apl2m * (apl2m + 1))
		a1 = (a2 + d2m*a1) * fnorm
		b1 = (b2 + d2m*b1) * fnorm
		a2 = a1 + d2m1*a2*fnorm
		b2 = b1 + d2m1*b2*fnorm
		if b2 != 0 {
			fnorm = 1 / b2
			cfnew = a2 * fnorm
			bfinished = math.Abs(cf-cfnew) < math.Abs(cf)*fMachEps
		}
		cf = cfnew
		rm++
	}
	return cf
}

// getLanczosSum uses a variant of the Lanczos sum with a rational function.
func getLanczosSum(fZ float64) float64 {
	num := []float64{
		23531376880.41075968857200767445163675473,
		42919803642.64909876895789904700198885093,
		35711959237.35566804944018545154716670596,
		17921034426.03720969991975575445893111267,
		6039542586.35202800506429164430729792107,
		1439720407.311721673663223072794912393972,
		248874557.8620541565114603864132294232163,
		31426415.58540019438061423162831820536287,
		2876370.628935372441225409051620849613599,
		186056.2653952234950402949897160456992822,
		8071.672002365816210638002902272250613822,
		210.8242777515793458725097339207133627117,
		2.506628274631000270164908177133837338626,
	}
	denom := []float64{
		0,
		39916800,
		120543840,
		150917976,
		105258076,
		45995730,
		13339535,
		2637558,
		357423,
		32670,
		1925,
		66,
		1,
	}
	var sumNum, sumDenom, zInv float64
	if fZ <= 1 {
		sumNum = num[12]
		sumDenom = denom[12]
		for i := 11; i >= 0; i-- {
			sumNum *= fZ
			sumNum += num[i]
			sumDenom *= fZ
			sumDenom += denom[i]
		}
	} else {
		zInv = 1 / fZ
		sumNum = num[0]
		sumDenom = denom[0]
		for i := 1; i <= 12; i++ {
			sumNum *= zInv
			sumNum += num[i]
			sumDenom *= zInv
			sumDenom += denom[i]
		}
	}
	return sumNum / sumDenom
}

// getBeta return beta distribution.
func getBeta(fAlpha, fBeta float64) float64 {
	var fA, fB float64
	if fAlpha > fBeta {
		fA = fAlpha
		fB = fBeta
	} else {
		fA = fBeta
		fB = fAlpha
	}
	const maxGammaArgument = 171.624376956302
	if fA+fB < maxGammaArgument {
		return math.Gamma(fA) / math.Gamma(fA+fB) * math.Gamma(fB)
	}
	fg := 6.024680040776729583740234375
	fgm := fg - 0.5
	fLanczos := getLanczosSum(fA)
	fLanczos /= getLanczosSum(fA + fB)
	fLanczos *= getLanczosSum(fB)
	fABgm := fA + fB + fgm
	fLanczos *= math.Sqrt((fABgm / (fA + fgm)) / (fB + fgm))
	fTempA := fB / (fA + fgm)
	fTempB := fA / (fB + fgm)
	fResult := math.Exp(-fA*math.Log1p(fTempA) - fB*math.Log1p(fTempB) - fgm)
	fResult *= fLanczos
	return fResult
}

// getBetaDistPDF is an implementation for the Beta probability density
// function.
func getBetaDistPDF(fX, fA, fB float64) float64 {
	if fX <= 0 || fX >= 1 {
		return 0
	}
	fLogDblMax, fLogDblMin := math.Log(1.79769e+308), math.Log(2.22507e-308)
	fLogY := math.Log(0.5 - fX + 0.5)
	if fX < 0.1 {
		fLogY = math.Log1p(-fX)
	}
	fLogX := math.Log(fX)
	fAm1LogX := (fA - 1) * fLogX
	fBm1LogY := (fB - 1) * fLogY
	fLogBeta := getLogBeta(fA, fB)
	if fAm1LogX < fLogDblMax && fAm1LogX > fLogDblMin && fBm1LogY < fLogDblMax &&
		fBm1LogY > fLogDblMin && fLogBeta < fLogDblMax && fLogBeta > fLogDblMin &&
		fAm1LogX+fBm1LogY < fLogDblMax && fAm1LogX+fBm1LogY > fLogDblMin {
		return math.Pow(fX, fA-1) * math.Pow(0.5-fX+0.5, fB-1) / getBeta(fA, fB)
	}
	return math.Exp(fAm1LogX + fBm1LogY - fLogBeta)
}

// getLogBeta return beta with logarithm.
func getLogBeta(fAlpha, fBeta float64) float64 {
	var fA, fB float64
	if fAlpha > fBeta {
		fA, fB = fAlpha, fBeta
	} else {
		fA, fB = fBeta, fAlpha
	}
	fg := 6.024680040776729583740234375
	fgm := fg - 0.5
	fLanczos := getLanczosSum(fA)
	fLanczos /= getLanczosSum(fA + fB)
	fLanczos *= getLanczosSum(fB)
	fLogLanczos := math.Log(fLanczos)
	fABgm := fA + fB + fgm
	fLogLanczos += 0.5 * (math.Log(fABgm) - math.Log(fA+fgm) - math.Log(fB+fgm))
	fTempA := fB / (fA + fgm)
	fTempB := fA / (fB + fgm)
	fResult := -fA*math.Log1p(fTempA) - fB*math.Log1p(fTempB) - fgm
	fResult += fLogLanczos
	return fResult
}

// getBetaDist is an implementation for the beta distribution function.
func getBetaDist(fXin, fAlpha, fBeta float64) float64 {
	if fXin <= 0 {
		return 0
	}
	if fXin >= 1 {
		return 1
	}
	if fBeta == 1 {
		return math.Pow(fXin, fAlpha)
	}
	if fAlpha == 1 {
		return -math.Expm1(fBeta * math.Log1p(-fXin))
	}
	var fResult float64
	fY, flnY := (0.5-fXin)+0.5, math.Log1p(-fXin)
	fX, flnX := fXin, math.Log(fXin)
	fA, fB := fAlpha, fBeta
	bReflect := fXin > fAlpha/(fAlpha+fBeta)
	if bReflect {
		fA = fBeta
		fB = fAlpha
		fX = fY
		fY = fXin
		flnX = flnY
		flnY = math.Log(fXin)
	}
	fResult = getBetaHelperContFrac(fX, fA, fB) / fA
	fP, fQ := fA/(fA+fB), fB/(fA+fB)
	var fTemp float64
	if fA > 1 && fB > 1 && fP < 0.97 && fQ < 0.97 {
		fTemp = getBetaDistPDF(fX, fA, fB) * fX * fY
	} else {
		fTemp = math.Exp(fA*flnX + fB*flnY - getLogBeta(fA, fB))
	}
	fResult *= fTemp
	if bReflect {
		fResult = 0.5 - fResult + 0.5
	}
	return fResult
}

// prepareBETAdotDISTArgs checking and prepare arguments for the formula
// function BETA.DIST.
func (fn *formulaFuncs) prepareBETAdotDISTArgs(argsList *list.List) formulaArg {
	if argsList.Len() < 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "BETA.DIST requires at least 4 arguments")
	}
	if argsList.Len() > 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "BETA.DIST requires at most 6 arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	alpha := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if alpha.Type != ArgNumber {
		return alpha
	}
	beta := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if beta.Type != ArgNumber {
		return beta
	}
	if alpha.Number <= 0 || beta.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	cumulative := argsList.Front().Next().Next().Next().Value.(formulaArg).ToBool()
	if cumulative.Type != ArgNumber {
		return cumulative
	}
	a, b := newNumberFormulaArg(0), newNumberFormulaArg(1)
	if argsList.Len() > 4 {
		if a = argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); a.Type != ArgNumber {
			return a
		}
	}
	if argsList.Len() == 6 {
		if b = argsList.Back().Value.(formulaArg).ToNumber(); b.Type != ArgNumber {
			return b
		}
	}
	return newListFormulaArg([]formulaArg{x, alpha, beta, cumulative, a, b})
}

// BETAdotDIST function calculates the cumulative beta distribution function
// or the probability density function of the Beta distribution, for a
// supplied set of parameters. The syntax of the function is:
//
//	BETA.DIST(x,alpha,beta,cumulative,[A],[B])
func (fn *formulaFuncs) BETAdotDIST(argsList *list.List) formulaArg {
	args := fn.prepareBETAdotDISTArgs(argsList)
	if args.Type != ArgList {
		return args
	}
	x, alpha, beta, cumulative, a, b := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5]
	if x.Number < a.Number || x.Number > b.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if a.Number == b.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	scale := b.Number - a.Number
	x.Number = (x.Number - a.Number) / scale
	if cumulative.Number == 1 {
		return newNumberFormulaArg(getBetaDist(x.Number, alpha.Number, beta.Number))
	}
	return newNumberFormulaArg(getBetaDistPDF(x.Number, alpha.Number, beta.Number) / scale)
}

// BETADIST function calculates the cumulative beta probability density
// function for a supplied set of parameters. The syntax of the function is:
//
//	BETADIST(x,alpha,beta,[A],[B])
func (fn *formulaFuncs) BETADIST(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "BETADIST requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "BETADIST requires at most 5 arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	alpha := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if alpha.Type != ArgNumber {
		return alpha
	}
	beta := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if beta.Type != ArgNumber {
		return beta
	}
	if alpha.Number <= 0 || beta.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	a, b := newNumberFormulaArg(0), newNumberFormulaArg(1)
	if argsList.Len() > 3 {
		if a = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); a.Type != ArgNumber {
			return a
		}
	}
	if argsList.Len() == 5 {
		if b = argsList.Back().Value.(formulaArg).ToNumber(); b.Type != ArgNumber {
			return b
		}
	}
	if x.Number < a.Number || x.Number > b.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if a.Number == b.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(getBetaDist((x.Number-a.Number)/(b.Number-a.Number), alpha.Number, beta.Number))
}

// d1mach returns double precision real machine constants.
func d1mach(i int) float64 {
	arr := []float64{
		2.2250738585072014e-308,
		1.7976931348623158e+308,
		1.1102230246251565e-16,
		2.2204460492503131e-16,
		0.301029995663981195,
	}
	if i > len(arr) {
		return 0
	}
	return arr[i-1]
}

// chebyshevInit determines the number of terms for the double precision
// orthogonal series "dos" needed to insure the error is no larger
// than "eta". Ordinarily eta will be chosen to be one-tenth machine
// precision.
func chebyshevInit(nos int, eta float64, dos []float64) int {
	i, e := 0, 0.0
	if nos < 1 {
		return 0
	}
	for ii := 1; ii <= nos; ii++ {
		i = nos - ii
		e += math.Abs(dos[i])
		if e > eta {
			return i
		}
	}
	return i
}

// chebyshevEval evaluates the n-term Chebyshev series "a" at "x".
func chebyshevEval(n int, x float64, a []float64) float64 {
	if n < 1 || n > 1000 || x < -1.1 || x > 1.1 {
		return math.NaN()
	}
	twox, b0, b1, b2 := x*2, 0.0, 0.0, 0.0
	for i := 1; i <= n; i++ {
		b2 = b1
		b1 = b0
		b0 = twox*b1 - b2 + a[n-i]
	}
	return (b0 - b2) * 0.5
}

// lgammacor is an implementation for the log(gamma) correction.
func lgammacor(x float64) float64 {
	algmcs := []float64{
		0.1666389480451863247205729650822, -0.1384948176067563840732986059135e-4,
		0.9810825646924729426157171547487e-8, -0.1809129475572494194263306266719e-10,
		0.6221098041892605227126015543416e-13, -0.3399615005417721944303330599666e-15,
		0.2683181998482698748957538846666e-17, -0.2868042435334643284144622399999e-19,
		0.3962837061046434803679306666666e-21, -0.6831888753985766870111999999999e-23,
		0.1429227355942498147573333333333e-24, -0.3547598158101070547199999999999e-26,
		0.1025680058010470912000000000000e-27, -0.3401102254316748799999999999999e-29,
		0.1276642195630062933333333333333e-30,
	}
	nalgm := chebyshevInit(15, d1mach(3), algmcs)
	xbig := 1.0 / math.Sqrt(d1mach(3))
	xmax := math.Exp(math.Min(math.Log(d1mach(2)/12.0), -math.Log(12.0*d1mach(1))))
	if x < 10.0 {
		return math.NaN()
	} else if x >= xmax {
		return 4.930380657631324e-32
	} else if x < xbig {
		tmp := 10.0 / x
		return chebyshevEval(nalgm, tmp*tmp*2.0-1.0, algmcs) / x
	}
	return 1.0 / (x * 12.0)
}

// logrelerr compute the relative error logarithm.
func logrelerr(x float64) float64 {
	alnrcs := []float64{
		0.10378693562743769800686267719098e+1, -0.13364301504908918098766041553133,
		0.19408249135520563357926199374750e-1, -0.30107551127535777690376537776592e-2,
		0.48694614797154850090456366509137e-3, -0.81054881893175356066809943008622e-4,
		0.13778847799559524782938251496059e-4, -0.23802210894358970251369992914935e-5,
		0.41640416213865183476391859901989e-6, -0.73595828378075994984266837031998e-7,
		0.13117611876241674949152294345011e-7, -0.23546709317742425136696092330175e-8,
		0.42522773276034997775638052962567e-9, -0.77190894134840796826108107493300e-10,
		0.14075746481359069909215356472191e-10, -0.25769072058024680627537078627584e-11,
		0.47342406666294421849154395005938e-12, -0.87249012674742641745301263292675e-13,
		0.16124614902740551465739833119115e-13, -0.29875652015665773006710792416815e-14,
		0.55480701209082887983041321697279e-15, -0.10324619158271569595141333961932e-15,
		0.19250239203049851177878503244868e-16, -0.35955073465265150011189707844266e-17,
		0.67264542537876857892194574226773e-18, -0.12602624168735219252082425637546e-18,
		0.23644884408606210044916158955519e-19, -0.44419377050807936898878389179733e-20,
		0.83546594464034259016241293994666e-21, -0.15731559416479562574899253521066e-21,
		0.29653128740247422686154369706666e-22, -0.55949583481815947292156013226666e-23,
		0.10566354268835681048187284138666e-23, -0.19972483680670204548314999466666e-24,
		0.37782977818839361421049855999999e-25, -0.71531586889081740345038165333333e-26,
		0.13552488463674213646502024533333e-26, -0.25694673048487567430079829333333e-27,
		0.48747756066216949076459519999999e-28, -0.92542112530849715321132373333333e-29,
		0.17578597841760239233269760000000e-29, -0.33410026677731010351377066666666e-30,
		0.63533936180236187354180266666666e-31,
	}
	nlnrel := chebyshevInit(43, 0.1*d1mach(3), alnrcs)
	if x <= -1 {
		return math.NaN()
	}
	if math.Abs(x) <= 0.375 {
		return x * (1.0 - x*chebyshevEval(nlnrel, x/0.375, alnrcs))
	}
	return math.Log(x + 1.0)
}

// logBeta is an implementation for the log of the beta distribution
// function.
func logBeta(a, b float64) float64 {
	corr, p, q := 0.0, a, a
	if b < p {
		p = b
	}
	if b > q {
		q = b
	}
	if p < 0 {
		return math.NaN()
	}
	if p == 0 {
		return math.MaxFloat64
	}
	if p >= 10.0 {
		corr = lgammacor(p) + lgammacor(q) - lgammacor(p+q)
		f1 := q * logrelerr(-p/(p+q))
		return math.Log(q)*-0.5 + 0.918938533204672741780329736406 + corr + (p-0.5)*math.Log(p/(p+q)) + math.Nextafter(f1, f1)
	}
	if q >= 10 {
		corr = lgammacor(q) - lgammacor(p+q)
		val, _ := math.Lgamma(p)
		return val + corr + p - p*math.Log(p+q) + (q-0.5)*logrelerr(-p/(p+q))
	}
	return math.Log(math.Gamma(p) * (math.Gamma(q) / math.Gamma(p+q)))
}

// pbetaRaw is a part of pbeta for the beta distribution.
func pbetaRaw(alnsml, ans, eps, p, pin, q, sml, x, y float64) float64 {
	if q > 1.0 {
		xb := p*math.Log(y) + q*math.Log(1.0-y) - logBeta(p, q) - math.Log(q)
		ib := int(math.Max(xb/alnsml, 0.0))
		term := math.Exp(xb - float64(ib)*alnsml)
		c := 1.0 / (1.0 - y)
		p1 := q * c / (p + q - 1.0)
		finsum := 0.0
		n := int(q)
		if q == float64(n) {
			n = n - 1
		}
		for i := 1; i <= n; i++ {
			if p1 <= 1 && term/eps <= finsum {
				break
			}
			xi := float64(i)
			term = (q - xi + 1.0) * c * term / (p + q - xi)
			if term > 1.0 {
				ib = ib - 1
				term = term * sml
			}
			if ib == 0 {
				finsum = finsum + term
			}
		}
		ans = ans + finsum
	}
	if y != x || p != pin {
		ans = 1.0 - ans
	}
	ans = math.Max(math.Min(ans, 1.0), 0.0)
	return ans
}

// pbeta returns distribution function of the beta distribution.
func pbeta(x, pin, qin float64) (ans float64) {
	eps := d1mach(3)
	alneps := math.Log(eps)
	sml := d1mach(1)
	alnsml := math.Log(sml)
	y := x
	p := pin
	q := qin
	if p/(p+q) < x {
		y = 1.0 - y
		p = qin
		q = pin
	}
	if (p+q)*y/(p+1.0) < eps {
		xb := p*math.Log(math.Max(y, sml)) - math.Log(p) - logBeta(p, q)
		if xb > alnsml && y != 0.0 {
			ans = math.Exp(xb)
		}
		if y != x || p != pin {
			ans = 1.0 - ans
		}
	} else {
		ps := q - math.Floor(q)
		if ps == 0.0 {
			ps = 1.0
		}
		xb := p*math.Log(y) - logBeta(ps, p) - math.Log(p)
		if xb >= alnsml {
			ans = math.Exp(xb)
			term := ans * p
			if ps != 1.0 {
				n := int(math.Max(alneps/math.Log(y), 4.0))
				for i := 1; i <= n; i++ {
					xi := float64(i)
					term = term * (xi - ps) * y / xi
					ans = ans + term/(p+xi)
				}
			}
		}
		ans = pbetaRaw(alnsml, ans, eps, p, pin, q, sml, x, y)
	}
	return ans
}

// betainvProbIterator is a part of betainv for the inverse of the beta
// function.
func betainvProbIterator(alpha1, alpha3, beta1, beta2, beta3, logBeta, maxCumulative, prob1, prob2 float64) float64 {
	var i, j, prev, prop4 float64
	j = 1
	for prob := 0; prob < 1000; prob++ {
		prop3 := pbeta(beta3, alpha1, beta1)
		prop3 = (prop3 - prob1) * math.Exp(logBeta+prob2*math.Log(beta3)+beta2*math.Log(1.0-beta3))
		if prop3*prop4 <= 0 {
			prev = math.Max(math.Abs(j), maxCumulative)
		}
		h := 1.0
		for iteratorCount := 0; iteratorCount < 1000; iteratorCount++ {
			j = h * prop3
			if math.Abs(j) < prev {
				i = beta3 - j
				if i >= 0 && i <= 1.0 {
					if prev <= alpha3 {
						return beta3
					}
					if math.Abs(prop3) <= alpha3 {
						return beta3
					}
					if i != 0 && i != 1.0 {
						break
					}
				}
			}
			h /= 3.0
		}
		if i == beta3 {
			return beta3
		}
		beta3, prop4 = i, prop3
	}
	return beta3
}

// calcBetainv is an implementation for the quantile of the beta
// distribution.
func calcBetainv(probability, alpha, beta, lower, upper float64) float64 {
	minCumulative, maxCumulative := 1.0e-300, 3.0e-308
	lowerBound, upperBound := maxCumulative, 1.0-2.22e-16
	needSwap := false
	var alpha1, alpha2, beta1, beta2, beta3, prob1, x, y float64
	if probability <= 0.5 {
		prob1, alpha1, beta1 = probability, alpha, beta
	} else {
		prob1, alpha1, beta1, needSwap = 1.0-probability, beta, alpha, true
	}
	logBetaNum := logBeta(alpha, beta)
	prob2 := math.Sqrt(-math.Log(prob1 * prob1))
	prob3 := prob2 - (prob2*0.27061+2.3075)/(prob2*(prob2*0.04481+0.99229)+1)
	if alpha1 > 1 && beta1 > 1 {
		alpha2, beta2, prob2 = 1/(alpha1+alpha1-1), 1/(beta1+beta1-1), (prob3*prob3-3)/6
		x = 2 / (alpha2 + beta2)
		y = prob3*math.Sqrt(x+prob2)/x - (beta2-alpha2)*(prob2+5/6.0-2/(x*3))
		beta3 = alpha1 / (alpha1 + beta1*math.Exp(y+y))
	} else {
		beta2, prob2 = 1/(beta1*9), beta1+beta1
		beta2 = prob2 * math.Pow(1-beta2+prob3*math.Sqrt(beta2), 3)
		if beta2 <= 0 {
			beta3 = 1 - math.Exp((math.Log((1-prob1)*beta1)+logBetaNum)/beta1)
		} else {
			beta2 = (prob2 + alpha1*4 - 2) / beta2
			if beta2 <= 1 {
				beta3 = math.Exp((logBetaNum + math.Log(alpha1*prob1)) / alpha1)
			} else {
				beta3 = 1 - 2/(beta2+1)
			}
		}
	}
	beta2, prob2 = 1-beta1, 1-alpha1
	if beta3 < lowerBound {
		beta3 = lowerBound
	} else if beta3 > upperBound {
		beta3 = upperBound
	}
	alpha3 := math.Max(minCumulative, math.Pow(10.0, -13.0-2.5/(alpha1*alpha1)-0.5/(prob1*prob1)))
	beta3 = betainvProbIterator(alpha1, alpha3, beta1, beta2, beta3, logBetaNum, maxCumulative, prob1, prob2)
	if needSwap {
		beta3 = 1.0 - beta3
	}
	return (upper-lower)*beta3 + lower
}

// betainv is an implementation of the formula functions BETAINV and
// BETA.INV.
func (fn *formulaFuncs) betainv(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 3 arguments", name))
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at most 5 arguments", name))
	}
	probability := argsList.Front().Value.(formulaArg).ToNumber()
	if probability.Type != ArgNumber {
		return probability
	}
	if probability.Number <= 0 || probability.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	alpha := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if alpha.Type != ArgNumber {
		return alpha
	}
	beta := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if beta.Type != ArgNumber {
		return beta
	}
	if alpha.Number <= 0 || beta.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	a, b := newNumberFormulaArg(0), newNumberFormulaArg(1)
	if argsList.Len() > 3 {
		if a = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); a.Type != ArgNumber {
			return a
		}
	}
	if argsList.Len() == 5 {
		if b = argsList.Back().Value.(formulaArg).ToNumber(); b.Type != ArgNumber {
			return b
		}
	}
	if a.Number == b.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(calcBetainv(probability.Number, alpha.Number, beta.Number, a.Number, b.Number))
}

// BETAINV function uses an iterative procedure to calculate the inverse of
// the cumulative beta probability density function for a supplied
// probability. The syntax of the function is:
//
//	BETAINV(probability,alpha,beta,[A],[B])
func (fn *formulaFuncs) BETAINV(argsList *list.List) formulaArg {
	return fn.betainv("BETAINV", argsList)
}

// BETAdotINV function uses an iterative procedure to calculate the inverse of
// the cumulative beta probability density function for a supplied
// probability. The syntax of the function is:
//
//	BETA.INV(probability,alpha,beta,[A],[B])
func (fn *formulaFuncs) BETAdotINV(argsList *list.List) formulaArg {
	return fn.betainv("BETA.INV", argsList)
}

// incompleteGamma is an implementation of the incomplete gamma function.
func incompleteGamma(a, x float64) float64 {
	max := 32
	summer := 0.0
	for n := 0; n <= max; n++ {
		divisor := a
		for i := 1; i <= n; i++ {
			divisor *= a + float64(i)
		}
		summer += math.Pow(x, float64(n)) / divisor
	}
	return math.Pow(x, a) * math.Exp(0-x) * summer
}

// binomCoeff implement binomial coefficient calculation.
func binomCoeff(n, k float64) float64 {
	return fact(n) / (fact(k) * fact(n-k))
}

// binomdist implement binomial distribution calculation.
func binomdist(x, n, p float64) float64 {
	return binomCoeff(n, x) * math.Pow(p, x) * math.Pow(1-p, n-x)
}

// BINOMdotDIST function returns the Binomial Distribution probability for a
// given number of successes from a specified number of trials. The syntax of
// the function is:
//
//	BINOM.DIST(number_s,trials,probability_s,cumulative)
func (fn *formulaFuncs) BINOMdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "BINOM.DIST requires 4 arguments")
	}
	return fn.BINOMDIST(argsList)
}

// BINOMDIST function returns the Binomial Distribution probability of a
// specified number of successes out of a specified number of trials. The
// syntax of the function is:
//
//	BINOMDIST(number_s,trials,probability_s,cumulative)
func (fn *formulaFuncs) BINOMDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "BINOMDIST requires 4 arguments")
	}
	var s, trials, probability, cumulative formulaArg
	if s = argsList.Front().Value.(formulaArg).ToNumber(); s.Type != ArgNumber {
		return s
	}
	if trials = argsList.Front().Next().Value.(formulaArg).ToNumber(); trials.Type != ArgNumber {
		return trials
	}
	if s.Number < 0 || s.Number > trials.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if probability = argsList.Back().Prev().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}

	if probability.Number < 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if cumulative.Number == 1 {
		bm := 0.0
		for i := 0; i <= int(s.Number); i++ {
			bm += binomdist(float64(i), trials.Number, probability.Number)
		}
		return newNumberFormulaArg(bm)
	}
	return newNumberFormulaArg(binomdist(s.Number, trials.Number, probability.Number))
}

// BINOMdotDISTdotRANGE function returns the Binomial Distribution probability
// for the number of successes from a specified number of trials falling into
// a specified range.
//
//	BINOM.DIST.RANGE(trials,probability_s,number_s,[number_s2])
func (fn *formulaFuncs) BINOMdotDISTdotRANGE(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "BINOM.DIST.RANGE requires at least 3 arguments")
	}
	if argsList.Len() > 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "BINOM.DIST.RANGE requires at most 4 arguments")
	}
	trials := argsList.Front().Value.(formulaArg).ToNumber()
	if trials.Type != ArgNumber {
		return trials
	}
	probability := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if probability.Type != ArgNumber {
		return probability
	}
	if probability.Number < 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	num1 := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if num1.Type != ArgNumber {
		return num1
	}
	if num1.Number < 0 || num1.Number > trials.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	num2 := num1
	if argsList.Len() > 3 {
		if num2 = argsList.Back().Value.(formulaArg).ToNumber(); num2.Type != ArgNumber {
			return num2
		}
	}
	if num2.Number < 0 || num2.Number > trials.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	sum := 0.0
	for i := num1.Number; i <= num2.Number; i++ {
		sum += binomdist(i, trials.Number, probability.Number)
	}
	return newNumberFormulaArg(sum)
}

// binominv implement inverse of the binomial distribution calculation.
func binominv(n, p, alpha float64) float64 {
	q, i, sum, max := 1-p, 0.0, 0.0, 0.0
	n = math.Floor(n)
	if q > p {
		factor := math.Pow(q, n)
		sum = factor
		for i = 0; i < n && sum < alpha; i++ {
			factor *= (n - i) / (i + 1) * p / q
			sum += factor
		}
		return i
	}
	factor := math.Pow(p, n)
	sum, max = 1-factor, n
	for i = 0; i < max && sum >= alpha; i++ {
		factor *= (n - i) / (i + 1) * q / p
		sum -= factor
	}
	return n - i
}

// BINOMdotINV function returns the inverse of the Cumulative Binomial
// Distribution. The syntax of the function is:
//
//	BINOM.INV(trials,probability_s,alpha)
func (fn *formulaFuncs) BINOMdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "BINOM.INV requires 3 numeric arguments")
	}
	trials := argsList.Front().Value.(formulaArg).ToNumber()
	if trials.Type != ArgNumber {
		return trials
	}
	if trials.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	probability := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if probability.Type != ArgNumber {
		return probability
	}
	if probability.Number <= 0 || probability.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	alpha := argsList.Back().Value.(formulaArg).ToNumber()
	if alpha.Type != ArgNumber {
		return alpha
	}
	if alpha.Number <= 0 || alpha.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(binominv(trials.Number, probability.Number, alpha.Number))
}

// CHIDIST function calculates the right-tailed probability of the chi-square
// distribution. The syntax of the function is:
//
//	CHIDIST(x,degrees_freedom)
func (fn *formulaFuncs) CHIDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHIDIST requires 2 numeric arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	degrees := argsList.Back().Value.(formulaArg).ToNumber()
	if degrees.Type != ArgNumber {
		return degrees
	}
	logSqrtPi, sqrtPi := math.Log(math.Sqrt(math.Pi)), 1/math.Sqrt(math.Pi)
	var e, s, z, c, y float64
	a, x1, even := x.Number/2, x.Number, int(degrees.Number)%2 == 0
	if degrees.Number > 1 {
		y = math.Exp(-a)
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg(-math.Sqrt(x1)))
	o := fn.NORMSDIST(args)
	s = 2 * o.Number
	if even {
		s = y
	}
	if degrees.Number > 2 {
		x1 = (degrees.Number - 1) / 2
		z = 0.5
		if even {
			z = 1
		}
		if a > 20 {
			e = logSqrtPi
			if even {
				e = 0
			}
			c = math.Log(a)
			for z <= x1 {
				e = math.Log(z) + e
				s += math.Exp(c*z - a - e)
				z++
			}
			return newNumberFormulaArg(s)
		}
		e = sqrtPi / math.Sqrt(a)
		if even {
			e = 1
		}
		c = 0
		for z <= x1 {
			e = e * (a / z)
			c = c + e
			z++
		}
		return newNumberFormulaArg(c*y + s)
	}
	return newNumberFormulaArg(s)
}

// CHIINV function calculates the inverse of the right-tailed probability of
// the Chi-Square Distribution. The syntax of the function is:
//
//	CHIINV(probability,deg_freedom)
func (fn *formulaFuncs) CHIINV(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHIINV requires 2 numeric arguments")
	}
	probability := argsList.Front().Value.(formulaArg).ToNumber()
	if probability.Type != ArgNumber {
		return probability
	}
	if probability.Number <= 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	deg := argsList.Back().Value.(formulaArg).ToNumber()
	if deg.Type != ArgNumber {
		return deg
	}
	if deg.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(gammainv(1-probability.Number, 0.5*deg.Number, 2.0))
}

// CHITEST function uses the chi-square test to calculate the probability that
// the differences between two supplied data sets (of observed and expected
// frequencies), are likely to be simply due to sampling error, or if they are
// likely to be real. The syntax of the function is:
//
//	CHITEST(actual_range,expected_range)
func (fn *formulaFuncs) CHITEST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHITEST requires 2 arguments")
	}
	actual, expected := argsList.Front().Value.(formulaArg), argsList.Back().Value.(formulaArg)
	actualList, expectedList := actual.ToList(), expected.ToList()
	rows := len(actual.Matrix)
	if rows == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	columns := len(actualList) / rows
	if len(actualList) != len(expectedList) || len(actualList) == 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var result float64
	var degrees int
	for i := 0; i < len(actualList); i++ {
		a, e := actualList[i].ToNumber(), expectedList[i].ToNumber()
		if a.Type == ArgNumber && e.Type == ArgNumber {
			if e.Number == 0 {
				return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
			}
			if e.Number < 0 {
				return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
			}
			result += (a.Number - e.Number) * (a.Number - e.Number) / e.Number
		}
	}
	if rows == 1 {
		degrees = columns - 1
	} else if columns == 1 {
		degrees = rows - 1
	} else {
		degrees = (columns - 1) * (rows - 1)
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg(result))
	args.PushBack(newNumberFormulaArg(float64(degrees)))
	return fn.CHIDIST(args)
}

// getGammaSeries calculates a power-series of the gamma function.
func getGammaSeries(fA, fX float64) float64 {
	var (
		fHalfMachEps = 2.22045e-016 / 2
		fDenomfactor = fA
		fSummand     = 1 / fA
		fSum         = fSummand
		nCount       = 1
	)
	for fSummand/fSum > fHalfMachEps && nCount <= 10000 {
		fDenomfactor = fDenomfactor + 1
		fSummand = fSummand * fX / fDenomfactor
		fSum = fSum + fSummand
		nCount = nCount + 1
	}
	return fSum
}

// getGammaContFraction returns continued fraction with odd items of the gamma
// function.
func getGammaContFraction(fA, fX float64) float64 {
	var (
		fBigInv      = 2.22045e-016
		fHalfMachEps = fBigInv / 2
		fBig         = 1 / fBigInv
		fCount       = 0.0
		fY           = 1 - fA
		fDenom       = fX + 2 - fA
		fPkm1        = fX + 1
		fPkm2        = 1.0
		fQkm1        = fDenom * fX
		fQkm2        = fX
		fApprox      = fPkm1 / fQkm1
		bFinished    = false
	)
	for !bFinished && fCount < 10000 {
		fCount = fCount + 1
		fY = fY + 1
		fDenom = fDenom + 2
		var (
			fNum = fY * fCount
			f1   = fPkm1 * fDenom
			f2   = fPkm2 * fNum
			fPk  = math.Nextafter(f1, f1) - math.Nextafter(f2, f2)
			f3   = fQkm1 * fDenom
			f4   = fQkm2 * fNum
			fQk  = math.Nextafter(f3, f3) - math.Nextafter(f4, f4)
		)
		if fQk != 0 {
			fR := fPk / fQk
			bFinished = math.Abs((fApprox-fR)/fR) <= fHalfMachEps
			fApprox = fR
		}
		fPkm2, fPkm1, fQkm2, fQkm1 = fPkm1, fPk, fQkm1, fQk
		if math.Abs(fPk) > fBig {
			// reduce a fraction does not change the value
			fPkm2 = fPkm2 * fBigInv
			fPkm1 = fPkm1 * fBigInv
			fQkm2 = fQkm2 * fBigInv
			fQkm1 = fQkm1 * fBigInv
		}
	}
	return fApprox
}

// getLogGammaHelper is a part of implementation of the function getLogGamma.
func getLogGammaHelper(fZ float64) float64 {
	_fg := 6.024680040776729583740234375
	zgHelp := fZ + _fg - 0.5
	return math.Log(getLanczosSum(fZ)) + (fZ-0.5)*math.Log(zgHelp) - zgHelp
}

// getGammaHelper is a part of implementation of the function getLogGamma.
func getGammaHelper(fZ float64) float64 {
	var (
		gamma  = getLanczosSum(fZ)
		fg     = 6.024680040776729583740234375
		zgHelp = fZ + fg - 0.5
		// avoid intermediate overflow
		halfpower = math.Pow(zgHelp, fZ/2-0.25)
	)
	gamma *= halfpower
	gamma /= math.Exp(zgHelp)
	gamma *= halfpower
	if fZ <= 20 && fZ == math.Floor(fZ) {
		gamma = math.Round(gamma)
	}
	return gamma
}

// getLogGamma calculates the natural logarithm of the gamma function.
func getLogGamma(fZ float64) float64 {
	fMaxGammaArgument := 171.624376956302
	if fZ >= fMaxGammaArgument {
		return getLogGammaHelper(fZ)
	}
	if fZ >= 1.0 {
		return math.Log(getGammaHelper(fZ))
	}
	if fZ >= 0.5 {
		return math.Log(getGammaHelper(fZ+1) / fZ)
	}
	return getLogGammaHelper(fZ+2) - math.Log(fZ+1) - math.Log(fZ)
}

// getLowRegIGamma returns lower regularized incomplete gamma function.
func getLowRegIGamma(fA, fX float64) float64 {
	lnFactor := fA*math.Log(fX) - fX - getLogGamma(fA)
	factor := math.Exp(lnFactor)
	if fX > fA+1 {
		return 1 - factor*getGammaContFraction(fA, fX)
	}
	return factor * getGammaSeries(fA, fX)
}

// getChiSqDistCDF returns left tail for the Chi-Square distribution.
func getChiSqDistCDF(fX, fDF float64) float64 {
	if fX <= 0 {
		return 0
	}
	return getLowRegIGamma(fDF/2, fX/2)
}

// getChiSqDistPDF calculates the probability density function for the
// Chi-Square distribution.
func getChiSqDistPDF(fX, fDF float64) float64 {
	if fDF*fX > 1391000 {
		return math.Exp((0.5*fDF-1)*math.Log(fX*0.5) - 0.5*fX - math.Log(2) - getLogGamma(0.5*fDF))
	}
	var fCount, fValue float64
	if math.Mod(fDF, 2) < 0.5 {
		fValue = 0.5
		fCount = 2
	} else {
		fValue = 1 / math.Sqrt(fX*2*math.Pi)
		fCount = 1
	}
	for fCount < fDF {
		fValue *= fX / fCount
		fCount += 2
	}
	if fX >= 1425 {
		fValue = math.Exp(math.Log(fValue) - fX/2)
	} else {
		fValue *= math.Exp(-fX / 2)
	}
	return fValue
}

// CHISQdotDIST function calculates the Probability Density Function or the
// Cumulative Distribution Function for the Chi-Square Distribution. The
// syntax of the function is:
//
//	CHISQ.DIST(x,degrees_freedom,cumulative)
func (fn *formulaFuncs) CHISQdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHISQ.DIST requires 3 arguments")
	}
	var x, degrees, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if degrees = argsList.Front().Next().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if x.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	maxDeg := math.Pow10(10)
	if degrees.Number < 1 || degrees.Number >= maxDeg {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(getChiSqDistCDF(x.Number, degrees.Number))
	}
	return newNumberFormulaArg(getChiSqDistPDF(x.Number, degrees.Number))
}

// CHISQdotDISTdotRT function calculates the right-tailed probability of the
// Chi-Square Distribution. The syntax of the function is:
//
//	CHISQ.DIST.RT(x,degrees_freedom)
func (fn *formulaFuncs) CHISQdotDISTdotRT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHISQ.DIST.RT requires 2 numeric arguments")
	}
	return fn.CHIDIST(argsList)
}

// CHISQdotTEST function performs the chi-square test on two supplied data sets
// (of observed and expected frequencies), and returns the probability that
// the differences between the sets are simply due to sampling error. The
// syntax of the function is:
//
//	CHISQ.TEST(actual_range,expected_range)
func (fn *formulaFuncs) CHISQdotTEST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHISQ.TEST requires 2 arguments")
	}
	return fn.CHITEST(argsList)
}

// hasChangeOfSign check if the sign has been changed.
func hasChangeOfSign(u, w float64) bool {
	return (u < 0 && w > 0) || (u > 0 && w < 0)
}

// calcInverseIterator directly maps the required parameters for inverse
// distribution functions.
type calcInverseIterator struct {
	name        string
	fp, fDF, nT float64
}

// callBack implements the callback function for the inverse iterator.
func (iterator *calcInverseIterator) callBack(x float64) float64 {
	if iterator.name == "CHISQ.INV" {
		return iterator.fp - getChiSqDistCDF(x, iterator.fDF)
	}
	return iterator.fp - getTDist(x, iterator.fDF, iterator.nT)
}

// inverseQuadraticInterpolation inverse quadratic interpolation with
// additional brackets.
func inverseQuadraticInterpolation(iterator calcInverseIterator, fAx, fAy, fBx, fBy float64) float64 {
	fYEps := 1.0e-307
	fXEps := 2.22045e-016
	fPx, fPy, fQx, fQy, fRx, fRy := fAx, fAy, fBx, fBy, fAx, fAy
	fSx := 0.5 * (fAx + fBx)
	bHasToInterpolate := true
	nCount := 0
	for nCount < 500 && math.Abs(fRy) > fYEps && (fBx-fAx) > math.Max(math.Abs(fAx), math.Abs(fBx))*fXEps {
		if bHasToInterpolate {
			if fPy != fQy && fQy != fRy && fRy != fPy {
				fSx = fPx*fRy*fQy/(fRy-fPy)/(fQy-fPy) + fRx*fQy*fPy/(fQy-fRy)/(fPy-fRy) +
					fQx*fPy*fRy/(fPy-fQy)/(fRy-fQy)
				bHasToInterpolate = (fAx < fSx) && (fSx < fBx)
			} else {
				bHasToInterpolate = false
			}
		}
		if !bHasToInterpolate {
			fSx = 0.5 * (fAx + fBx)
			fQx, fQy = fBx, fBy
			bHasToInterpolate = true
		}
		fPx, fQx, fRx, fPy, fQy = fQx, fRx, fSx, fQy, fRy
		fRy = iterator.callBack(fSx)
		if hasChangeOfSign(fAy, fRy) {
			fBx, fBy = fRx, fRy
		} else {
			fAx, fAy = fRx, fRy
		}
		bHasToInterpolate = bHasToInterpolate && (math.Abs(fRy)*2 <= math.Abs(fQy))
		nCount++
	}
	return fRx
}

// calcIterateInverse function calculates the iteration for inverse
// distributions.
func calcIterateInverse(iterator calcInverseIterator, fAx, fBx float64) float64 {
	fAy, fBy := iterator.callBack(fAx), iterator.callBack(fBx)
	var fTemp float64
	var nCount int
	for nCount = 0; nCount < 1000 && !hasChangeOfSign(fAy, fBy); nCount++ {
		if math.Abs(fAy) <= math.Abs(fBy) {
			fTemp = fAx
			fAx += 2 * (fAx - fBx)
			if fAx < 0 {
				fAx = 0
			}
			fBx = fTemp
			fBy = fAy
			fAy = iterator.callBack(fAx)
		} else {
			fTemp = fBx
			fBx += 2 * (fBx - fAx)
			fAx = fTemp
			fAy = fBy
			fBy = iterator.callBack(fBx)
		}
	}
	if fAy == 0 || fBy == 0 {
		return 0
	}
	return inverseQuadraticInterpolation(iterator, fAx, fAy, fBx, fBy)
}

// CHISQdotINV function calculates the inverse of the left-tailed probability
// of the Chi-Square Distribution. The syntax of the function is:
//
//	CHISQ.INV(probability,degrees_freedom)
func (fn *formulaFuncs) CHISQdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHISQ.INV requires 2 numeric arguments")
	}
	var probability, degrees formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if degrees = argsList.Back().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if probability.Number < 0 || probability.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if degrees.Number < 1 || degrees.Number > math.Pow10(10) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(calcIterateInverse(calcInverseIterator{
		name: "CHISQ.INV",
		fp:   probability.Number,
		fDF:  degrees.Number,
	}, degrees.Number/2, degrees.Number))
}

// CHISQdotINVdotRT function calculates the inverse of the right-tailed
// probability of the Chi-Square Distribution. The syntax of the function is:
//
//	CHISQ.INV.RT(probability,degrees_freedom)
func (fn *formulaFuncs) CHISQdotINVdotRT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHISQ.INV.RT requires 2 numeric arguments")
	}
	return fn.CHIINV(argsList)
}

// confidence is an implementation of the formula functions CONFIDENCE and
// CONFIDENCE.NORM.
func (fn *formulaFuncs) confidence(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 3 numeric arguments", name))
	}
	alpha := argsList.Front().Value.(formulaArg).ToNumber()
	if alpha.Type != ArgNumber {
		return alpha
	}
	if alpha.Number <= 0 || alpha.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	stdDev := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if stdDev.Type != ArgNumber {
		return stdDev
	}
	if stdDev.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	size := argsList.Back().Value.(formulaArg).ToNumber()
	if size.Type != ArgNumber {
		return size
	}
	if size.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New()
	args.Init()
	args.PushBack(newNumberFormulaArg(alpha.Number / 2))
	args.PushBack(newNumberFormulaArg(0))
	args.PushBack(newNumberFormulaArg(1))
	return newNumberFormulaArg(-fn.NORMINV(args).Number * (stdDev.Number / math.Sqrt(size.Number)))
}

// CONFIDENCE function uses a Normal Distribution to calculate a confidence
// value that can be used to construct the Confidence Interval for a
// population mean, for a supplied probability and sample size. It is assumed
// that the standard deviation of the population is known. The syntax of the
// function is:
//
//	CONFIDENCE(alpha,standard_dev,size)
func (fn *formulaFuncs) CONFIDENCE(argsList *list.List) formulaArg {
	return fn.confidence("CONFIDENCE", argsList)
}

// CONFIDENCEdotNORM function uses a Normal Distribution to calculate a
// confidence value that can be used to construct the confidence interval for
// a population mean, for a supplied probability and sample size. It is
// assumed that the standard deviation of the population is known. The syntax
// of the function is:
//
//	CONFIDENCE.NORM(alpha,standard_dev,size)
func (fn *formulaFuncs) CONFIDENCEdotNORM(argsList *list.List) formulaArg {
	return fn.confidence("CONFIDENCE.NORM", argsList)
}

// CONFIDENCEdotT function uses a Student's T-Distribution to calculate a
// confidence value that can be used to construct the confidence interval for
// a population mean, for a supplied probablity and supplied sample size. It
// is assumed that the standard deviation of the population is known. The
// syntax of the function is:
//
//	CONFIDENCE.T(alpha,standard_dev,size)
func (fn *formulaFuncs) CONFIDENCEdotT(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "CONFIDENCE.T requires 3 arguments")
	}
	var alpha, standardDev, size formulaArg
	if alpha = argsList.Front().Value.(formulaArg).ToNumber(); alpha.Type != ArgNumber {
		return alpha
	}
	if standardDev = argsList.Front().Next().Value.(formulaArg).ToNumber(); standardDev.Type != ArgNumber {
		return standardDev
	}
	if size = argsList.Back().Value.(formulaArg).ToNumber(); size.Type != ArgNumber {
		return size
	}
	if alpha.Number <= 0 || alpha.Number >= 1 || standardDev.Number <= 0 || size.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if size.Number == 1 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(standardDev.Number * calcIterateInverse(calcInverseIterator{
		name: "CONFIDENCE.T",
		fp:   alpha.Number,
		fDF:  size.Number - 1,
		nT:   2,
	}, size.Number/2, size.Number) / math.Sqrt(size.Number))
}

// covar is an implementation of the formula functions COVAR, COVARIANCE.P and
// COVARIANCE.S.
func (fn *formulaFuncs) covar(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 arguments", name))
	}
	array1 := argsList.Front().Value.(formulaArg)
	array2 := argsList.Back().Value.(formulaArg)
	left, right := array1.ToList(), array2.ToList()
	n := len(left)
	if n != len(right) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	l1, l2 := list.New(), list.New()
	l1.PushBack(array1)
	l2.PushBack(array2)
	result, skip := 0.0, 0
	mean1, mean2 := fn.AVERAGE(l1), fn.AVERAGE(l2)
	for i := 0; i < n; i++ {
		arg1 := left[i].ToNumber()
		arg2 := right[i].ToNumber()
		if arg1.Type == ArgError || arg2.Type == ArgError {
			skip++
			continue
		}
		result += (arg1.Number - mean1.Number) * (arg2.Number - mean2.Number)
	}
	if name == "COVARIANCE.S" {
		return newNumberFormulaArg(result / float64(n-skip-1))
	}
	return newNumberFormulaArg(result / float64(n-skip))
}

// COVAR function calculates the covariance of two supplied sets of values. The
// syntax of the function is:
//
//	COVAR(array1,array2)
func (fn *formulaFuncs) COVAR(argsList *list.List) formulaArg {
	return fn.covar("COVAR", argsList)
}

// COVARIANCEdotP function calculates the population covariance of two supplied
// sets of values. The syntax of the function is:
//
//	COVARIANCE.P(array1,array2)
func (fn *formulaFuncs) COVARIANCEdotP(argsList *list.List) formulaArg {
	return fn.covar("COVARIANCE.P", argsList)
}

// COVARIANCEdotS function calculates the sample covariance of two supplied
// sets of values. The syntax of the function is:
//
//	COVARIANCE.S(array1,array2)
func (fn *formulaFuncs) COVARIANCEdotS(argsList *list.List) formulaArg {
	return fn.covar("COVARIANCE.S", argsList)
}

// calcStringCountSum is part of the implementation countSum.
func calcStringCountSum(countText bool, count, sum float64, num, arg formulaArg) (float64, float64) {
	if countText && num.Type == ArgError && arg.String != "" {
		count++
	}
	if num.Type == ArgNumber {
		sum += num.Number
		count++
	}
	return count, sum
}

// countSum get count and sum for a formula arguments array.
func (fn *formulaFuncs) countSum(countText bool, args []formulaArg) (count, sum float64) {
	for _, arg := range args {
		switch arg.Type {
		case ArgNumber:
			if countText || !arg.Boolean {
				sum += arg.Number
				count++
			}
		case ArgString:
			if !countText && (arg.Value() == "TRUE" || arg.Value() == "FALSE") {
				continue
			} else if countText && (arg.Value() == "TRUE" || arg.Value() == "FALSE") {
				num := arg.ToBool()
				if num.Type == ArgNumber {
					count++
					sum += num.Number
					continue
				}
			}
			num := arg.ToNumber()
			count, sum = calcStringCountSum(countText, count, sum, num, arg)
		case ArgList, ArgMatrix:
			cnt, summary := fn.countSum(countText, arg.ToList())
			sum += summary
			count += cnt
		}
	}
	return
}

// CORREL function calculates the Pearson Product-Moment Correlation
// Coefficient for two sets of values. The syntax of the function is:
//
//	CORREL(array1,array2)
func (fn *formulaFuncs) CORREL(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CORREL requires 2 arguments")
	}
	array1 := argsList.Front().Value.(formulaArg)
	array2 := argsList.Back().Value.(formulaArg)
	left, right := array1.ToList(), array2.ToList()
	n := len(left)
	if n != len(right) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	l1, l2, l3 := list.New(), list.New(), list.New()
	for i := 0; i < n; i++ {
		if lhs, rhs := left[i].ToNumber(), right[i].ToNumber(); lhs.Number != 0 && rhs.Number != 0 {
			l1.PushBack(lhs)
			l2.PushBack(rhs)
		}
	}
	stdev1, stdev2 := fn.STDEV(l1), fn.STDEV(l2)
	if stdev1.Number == 0 || stdev2.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	mean1, mean2, skip := fn.AVERAGE(l1), fn.AVERAGE(l2), 0
	for i := 0; i < n; i++ {
		lhs, rhs := left[i].ToNumber(), right[i].ToNumber()
		if lhs.Number == 0 || rhs.Number == 0 {
			skip++
			continue
		}
		l3.PushBack(newNumberFormulaArg((lhs.Number - mean1.Number) * (rhs.Number - mean2.Number)))
	}
	return newNumberFormulaArg(fn.SUM(l3).Number / float64(n-skip-1) / stdev1.Number / stdev2.Number)
}

// COUNT function returns the count of numeric values in a supplied set of
// cells or values. This count includes both numbers and dates. The syntax of
// the function is:
//
//	COUNT(value1,[value2],...)
func (fn *formulaFuncs) COUNT(argsList *list.List) formulaArg {
	var count int
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			if num := arg.ToNumber(); num.Type == ArgNumber {
				count++
			}
		case ArgNumber:
			count++
		case ArgMatrix:
			for _, row := range arg.Matrix {
				for _, cell := range row {
					if cell.Type == ArgNumber {
						count++
					}
				}
			}
		}
	}
	return newNumberFormulaArg(float64(count))
}

// COUNTA function returns the number of non-blanks within a supplied set of
// cells or values. The syntax of the function is:
//
//	COUNTA(value1,[value2],...)
func (fn *formulaFuncs) COUNTA(argsList *list.List) formulaArg {
	var count int
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			if arg.String != "" {
				count++
			}
		case ArgNumber:
			count++
		case ArgMatrix:
			for _, row := range arg.ToList() {
				switch row.Type {
				case ArgString:
					if row.String != "" {
						count++
					}
				case ArgNumber:
					count++
				}
			}
		}
	}
	return newNumberFormulaArg(float64(count))
}

// COUNTBLANK function returns the number of blank cells in a supplied range.
// The syntax of the function is:
//
//	COUNTBLANK(range)
func (fn *formulaFuncs) COUNTBLANK(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COUNTBLANK requires 1 argument")
	}
	var count float64
	for _, cell := range argsList.Front().Value.(formulaArg).ToList() {
		if cell.Type == ArgEmpty {
			count++
		}
	}
	return newNumberFormulaArg(count)
}

// COUNTIF function returns the number of cells within a supplied range, that
// satisfy a given criteria. The syntax of the function is:
//
//	COUNTIF(range,criteria)
func (fn *formulaFuncs) COUNTIF(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "COUNTIF requires 2 arguments")
	}
	var (
		criteria = formulaCriteriaParser(argsList.Front().Next().Value.(formulaArg))
		count    float64
	)
	for _, cell := range argsList.Front().Value.(formulaArg).ToList() {
		if cell.Type == ArgString && criteria.Condition.Type != ArgString {
			continue
		}
		if ok, _ := formulaCriteriaEval(cell, criteria); ok {
			count++
		}
	}
	return newNumberFormulaArg(count)
}

// formulaIfsMatch function returns cells reference array which match criteria.
func formulaIfsMatch(args []formulaArg) (cellRefs []cellRef) {
	for i := 0; i < len(args)-1; i += 2 {
		var match []cellRef
		matrix, criteria := args[i].Matrix, formulaCriteriaParser(args[i+1])
		if i == 0 {
			for rowIdx, row := range matrix {
				for colIdx, col := range row {
					if ok, _ := formulaCriteriaEval(col, criteria); ok {
						match = append(match, cellRef{Col: colIdx, Row: rowIdx})
					}
				}
			}
		} else {
			match = []cellRef{}
			for _, ref := range cellRefs {
				value := matrix[ref.Row][ref.Col]
				if ok, _ := formulaCriteriaEval(value, criteria); ok {
					match = append(match, ref)
				}
			}
		}
		cellRefs = match[:]
	}
	return
}

// COUNTIFS function returns the number of rows within a table, that satisfy a
// set of given criteria. The syntax of the function is:
//
//	COUNTIFS(criteria_range1,criteria1,[criteria_range2,criteria2],...)
func (fn *formulaFuncs) COUNTIFS(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "COUNTIFS requires at least 2 arguments")
	}
	if argsList.Len()%2 != 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var args []formulaArg
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	return newNumberFormulaArg(float64(len(formulaIfsMatch(args))))
}

// CRITBINOM function returns the inverse of the Cumulative Binomial
// Distribution. I.e. for a specific number of independent trials, the
// function returns the smallest value (number of successes) for which the
// cumulative binomial distribution is greater than or equal to a specified
// value. The syntax of the function is:
//
//	CRITBINOM(trials,probability_s,alpha)
func (fn *formulaFuncs) CRITBINOM(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "CRITBINOM requires 3 numeric arguments")
	}
	return fn.BINOMdotINV(argsList)
}

// DEVSQ function calculates the sum of the squared deviations from the sample
// mean. The syntax of the function is:
//
//	DEVSQ(number1,[number2],...)
func (fn *formulaFuncs) DEVSQ(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "DEVSQ requires at least 1 numeric argument")
	}
	avg, count, result := fn.AVERAGE(argsList), -1, 0.0
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		for _, cell := range arg.Value.(formulaArg).ToList() {
			if cell.Type != ArgNumber {
				continue
			}
			count++
			if count == 0 {
				result = math.Pow(cell.Number-avg.Number, 2)
				continue
			}
			result += math.Pow(cell.Number-avg.Number, 2)
		}
	}
	if count == -1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(result)
}

// FISHER function calculates the Fisher Transformation for a supplied value.
// The syntax of the function is:
//
//	FISHER(x)
func (fn *formulaFuncs) FISHER(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FISHER requires 1 numeric argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgString:
		arg := token.ToNumber()
		if arg.Type == ArgNumber {
			if arg.Number <= -1 || arg.Number >= 1 {
				return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
			}
			return newNumberFormulaArg(0.5 * math.Log((1+arg.Number)/(1-arg.Number)))
		}
	case ArgNumber:
		if token.Number <= -1 || token.Number >= 1 {
			return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
		}
		return newNumberFormulaArg(0.5 * math.Log((1+token.Number)/(1-token.Number)))
	}
	return newErrorFormulaArg(formulaErrorVALUE, "FISHER requires 1 numeric argument")
}

// FISHERINV function calculates the inverse of the Fisher Transformation and
// returns a value between -1 and +1. The syntax of the function is:
//
//	FISHERINV(y)
func (fn *formulaFuncs) FISHERINV(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FISHERINV requires 1 numeric argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgString:
		arg := token.ToNumber()
		if arg.Type == ArgNumber {
			return newNumberFormulaArg((math.Exp(2*arg.Number) - 1) / (math.Exp(2*arg.Number) + 1))
		}
	case ArgNumber:
		return newNumberFormulaArg((math.Exp(2*token.Number) - 1) / (math.Exp(2*token.Number) + 1))
	}
	return newErrorFormulaArg(formulaErrorVALUE, "FISHERINV requires 1 numeric argument")
}

// FORECAST function predicts a future point on a linear trend line fitted to a
// supplied set of x- and y- values. The syntax of the function is:
//
//	FORECAST(x,known_y's,known_x's)
func (fn *formulaFuncs) FORECAST(argsList *list.List) formulaArg {
	return fn.pearsonProduct("FORECAST", 3, argsList)
}

// FORECASTdotLINEAR function predicts a future point on a linear trend line
// fitted to a supplied set of x- and y- values. The syntax of the function is:
//
//	FORECAST.LINEAR(x,known_y's,known_x's)
func (fn *formulaFuncs) FORECASTdotLINEAR(argsList *list.List) formulaArg {
	return fn.pearsonProduct("FORECAST.LINEAR", 3, argsList)
}

// maritxToSortedColumnList convert matrix formula arguments to a ascending
// order list by column.
func maritxToSortedColumnList(arg formulaArg) formulaArg {
	mtx, cols := []formulaArg{}, len(arg.Matrix[0])
	for colIdx := 0; colIdx < cols; colIdx++ {
		for _, row := range arg.Matrix {
			cell := row[colIdx]
			if cell.Type == ArgError {
				return cell
			}
			if cell.Type == ArgNumber {
				mtx = append(mtx, cell)
			}
		}
	}
	argsList := newListFormulaArg(mtx)
	sort.Slice(argsList.List, func(i, j int) bool {
		return argsList.List[i].Number < argsList.List[j].Number
	})
	return argsList
}

// FREQUENCY function to count how many children fall into different age
// ranges. The syntax of the function is:
//
//	FREQUENCY(data_array,bins_array)
func (fn *formulaFuncs) FREQUENCY(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "FREQUENCY requires 2 arguments")
	}
	data, bins := argsList.Front().Value.(formulaArg), argsList.Back().Value.(formulaArg)
	if len(data.Matrix) == 0 {
		data.Matrix = [][]formulaArg{{data}}
	}
	if len(bins.Matrix) == 0 {
		bins.Matrix = [][]formulaArg{{bins}}
	}
	var (
		dataMtx, binsMtx formulaArg
		c                [][]formulaArg
		i, j             int
	)
	if dataMtx = maritxToSortedColumnList(data); dataMtx.Type != ArgList {
		return dataMtx
	}
	if binsMtx = maritxToSortedColumnList(bins); binsMtx.Type != ArgList {
		return binsMtx
	}
	for row := 0; row < len(binsMtx.List)+1; row++ {
		rows := []formulaArg{}
		for col := 0; col < 1; col++ {
			rows = append(rows, newNumberFormulaArg(0))
		}
		c = append(c, rows)
	}
	for j = 0; j < len(binsMtx.List); j++ {
		n := 0.0
		for i < len(dataMtx.List) && dataMtx.List[i].Number <= binsMtx.List[j].Number {
			n++
			i++
		}
		c[j] = []formulaArg{newNumberFormulaArg(n)}
	}
	c[j] = []formulaArg{newNumberFormulaArg(float64(len(dataMtx.List) - i))}
	if len(c) > 2 {
		c[1], c[2] = c[2], c[1]
	}
	return newMatrixFormulaArg(c)
}

// GAMMA function returns the value of the Gamma Function, Γ(n), for a
// specified number, n. The syntax of the function is:
//
//	GAMMA(number)
func (fn *formulaFuncs) GAMMA(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMA requires 1 numeric argument")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMA requires 1 numeric argument")
	}
	if number.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(math.Gamma(number.Number))
}

// GAMMAdotDIST function returns the Gamma Distribution, which is frequently
// used to provide probabilities for values that may have a skewed
// distribution, such as queuing analysis.
//
//	GAMMA.DIST(x,alpha,beta,cumulative)
func (fn *formulaFuncs) GAMMAdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMA.DIST requires 4 arguments")
	}
	return fn.GAMMADIST(argsList)
}

// GAMMADIST function returns the Gamma Distribution, which is frequently used
// to provide probabilities for values that may have a skewed distribution,
// such as queuing analysis.
//
//	GAMMADIST(x,alpha,beta,cumulative)
func (fn *formulaFuncs) GAMMADIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMADIST requires 4 arguments")
	}
	var x, alpha, beta, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if x.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if alpha = argsList.Front().Next().Value.(formulaArg).ToNumber(); alpha.Type != ArgNumber {
		return alpha
	}
	if beta = argsList.Back().Prev().Value.(formulaArg).ToNumber(); beta.Type != ArgNumber {
		return beta
	}
	if alpha.Number <= 0 || beta.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(incompleteGamma(alpha.Number, x.Number/beta.Number) / math.Gamma(alpha.Number))
	}
	return newNumberFormulaArg((1 / (math.Pow(beta.Number, alpha.Number) * math.Gamma(alpha.Number))) * math.Pow(x.Number, alpha.Number-1) * math.Exp(0-(x.Number/beta.Number)))
}

// gammainv returns the inverse of the Gamma distribution for the specified
// value.
func gammainv(probability, alpha, beta float64) float64 {
	xLo, xHi := 0.0, alpha*beta*5
	dx, x, xNew, result := 1024.0, 1.0, 1.0, 0.0
	for i := 0; math.Abs(dx) > 8.88e-016 && i <= 256; i++ {
		result = incompleteGamma(alpha, x/beta) / math.Gamma(alpha)
		e := result - probability
		if e == 0 {
			dx = 0
		} else if e < 0 {
			xLo = x
		} else {
			xHi = x
		}
		pdf := (1 / (math.Pow(beta, alpha) * math.Gamma(alpha))) * math.Pow(x, alpha-1) * math.Exp(0-(x/beta))
		if pdf != 0 {
			dx = e / pdf
			xNew = x - dx
		}
		if xNew < xLo || xNew > xHi || pdf == 0 {
			xNew = (xLo + xHi) / 2
			dx = xNew - x
		}
		x = xNew
	}
	return x
}

// GAMMAdotINV function returns the inverse of the Gamma Cumulative
// Distribution. The syntax of the function is:
//
//	GAMMA.INV(probability,alpha,beta)
func (fn *formulaFuncs) GAMMAdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMA.INV requires 3 arguments")
	}
	return fn.GAMMAINV(argsList)
}

// GAMMAINV function returns the inverse of the Gamma Cumulative Distribution.
// The syntax of the function is:
//
//	GAMMAINV(probability,alpha,beta)
func (fn *formulaFuncs) GAMMAINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMAINV requires 3 arguments")
	}
	var probability, alpha, beta formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if probability.Number < 0 || probability.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if alpha = argsList.Front().Next().Value.(formulaArg).ToNumber(); alpha.Type != ArgNumber {
		return alpha
	}
	if beta = argsList.Back().Value.(formulaArg).ToNumber(); beta.Type != ArgNumber {
		return beta
	}
	if alpha.Number <= 0 || beta.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(gammainv(probability.Number, alpha.Number, beta.Number))
}

// GAMMALN function returns the natural logarithm of the Gamma Function, Γ
// (n). The syntax of the function is:
//
//	GAMMALN(x)
func (fn *formulaFuncs) GAMMALN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMALN requires 1 numeric argument")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMALN requires 1 numeric argument")
	}
	if x.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(math.Log(math.Gamma(x.Number)))
}

// GAMMALNdotPRECISE function returns the natural logarithm of the Gamma
// Function, Γ(n). The syntax of the function is:
//
//	GAMMALN.PRECISE(x)
func (fn *formulaFuncs) GAMMALNdotPRECISE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAMMALN.PRECISE requires 1 numeric argument")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	if x.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(getLogGamma(x.Number))
}

// GAUSS function returns the probability that a member of a standard normal
// population will fall between the mean and a specified number of standard
// deviations from the mean. The syntax of the function is:
//
//	GAUSS(z)
func (fn *formulaFuncs) GAUSS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GAUSS requires 1 numeric argument")
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(formulaArg{Type: ArgNumber, Number: 0})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1})
	args.PushBack(newBoolFormulaArg(true))
	normdist := fn.NORMDIST(args)
	if normdist.Type != ArgNumber {
		return normdist
	}
	return newNumberFormulaArg(normdist.Number - 0.5)
}

// GEOMEAN function calculates the geometric mean of a supplied set of values.
// The syntax of the function is:
//
//	GEOMEAN(number1,[number2],...)
func (fn *formulaFuncs) GEOMEAN(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "GEOMEAN requires at least 1 numeric argument")
	}
	product := fn.PRODUCT(argsList)
	if product.Type != ArgNumber {
		return product
	}
	count := fn.COUNT(argsList)
	min := fn.MIN(argsList)
	if product.Number > 0 && min.Number > 0 {
		return newNumberFormulaArg(math.Pow(product.Number, 1/count.Number))
	}
	return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
}

// getNewMatrix create matrix by given columns and rows.
func getNewMatrix(c, r int) (matrix [][]float64) {
	for i := 0; i < c; i++ {
		for j := 0; j < r; j++ {
			for x := len(matrix); x <= i; x++ {
				matrix = append(matrix, []float64{})
			}
			for y := len(matrix[i]); y <= j; y++ {
				matrix[i] = append(matrix[i], 0)
			}
			matrix[i][j] = 0
		}
	}
	return
}

// approxSub subtract two values, if signs are identical and the values are
// equal, will be returns 0 instead of calculating the subtraction.
func approxSub(a, b float64) float64 {
	if ((a < 0 && b < 0) || (a > 0 && b > 0)) && math.Abs(a-b) < 2.22045e-016 {
		return 0
	}
	return a - b
}

// matrixClone return a copy of all elements of the original matrix.
func matrixClone(matrix [][]float64) (cloneMatrix [][]float64) {
	for i := 0; i < len(matrix); i++ {
		for j := 0; j < len(matrix[i]); j++ {
			for x := len(cloneMatrix); x <= i; x++ {
				cloneMatrix = append(cloneMatrix, []float64{})
			}
			for k := len(cloneMatrix[i]); k <= j; k++ {
				cloneMatrix[i] = append(cloneMatrix[i], 0)
			}
			cloneMatrix[i][j] = matrix[i][j]
		}
	}
	return
}

// trendGrowthMatrixInfo defined matrix checking result.
type trendGrowthMatrixInfo struct {
	trendType, nCX, nCY, nRX, nRY, M, N int
	mtxX, mtxY                          [][]float64
}

// prepareTrendGrowthMtxX is a part of implementation of the trend growth prepare.
func prepareTrendGrowthMtxX(mtxX [][]float64) [][]float64 {
	var mtx [][]float64
	for i := 0; i < len(mtxX); i++ {
		for j := 0; j < len(mtxX[i]); j++ {
			if mtxX[i][j] == 0 {
				return nil
			}
			for x := len(mtx); x <= j; x++ {
				mtx = append(mtx, []float64{})
			}
			for y := len(mtx[j]); y <= i; y++ {
				mtx[j] = append(mtx[j], 0)
			}
			mtx[j][i] = mtxX[i][j]
		}
	}
	return mtx
}

// prepareTrendGrowthMtxY is a part of implementation of the trend growth prepare.
func prepareTrendGrowthMtxY(bLOG bool, mtxY [][]float64) [][]float64 {
	var mtx [][]float64
	for i := 0; i < len(mtxY); i++ {
		for j := 0; j < len(mtxY[i]); j++ {
			if mtxY[i][j] == 0 {
				return nil
			}
			for x := len(mtx); x <= j; x++ {
				mtx = append(mtx, []float64{})
			}
			for y := len(mtx[j]); y <= i; y++ {
				mtx[j] = append(mtx[j], 0)
			}
			mtx[j][i] = mtxY[i][j]
		}
	}
	if bLOG {
		var pNewY [][]float64
		for i := 0; i < len(mtxY); i++ {
			for j := 0; j < len(mtxY[i]); j++ {
				fVal := mtxY[i][j]
				if fVal <= 0 {
					return nil
				}
				for x := len(pNewY); x <= j; x++ {
					pNewY = append(pNewY, []float64{})
				}
				for y := len(pNewY[j]); y <= i; y++ {
					pNewY[j] = append(pNewY[j], 0)
				}
				pNewY[j][i] = math.Log(fVal)
			}
		}
		mtx = pNewY
	}
	return mtx
}

// prepareTrendGrowth check and return the result.
func prepareTrendGrowth(bLOG bool, mtxX, mtxY [][]float64) (*trendGrowthMatrixInfo, formulaArg) {
	var nCX, nRX, M, N, trendType int
	nRY, nCY := len(mtxY), len(mtxY[0])
	cntY := nCY * nRY
	newY := prepareTrendGrowthMtxY(bLOG, mtxY)
	if newY == nil {
		return nil, newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	var newX [][]float64
	if len(mtxX) != 0 {
		nRX, nCX = len(mtxX), len(mtxX[0])
		if newX = prepareTrendGrowthMtxX(mtxX); newX == nil {
			return nil, newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		if nCX == nCY && nRX == nRY {
			trendType, M, N = 1, 1, cntY // simple regression
		} else if nCY != 1 && nRY != 1 {
			return nil, newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
		} else if nCY == 1 {
			if nRX != nRY {
				return nil, newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
			}
			trendType, M, N = 2, nCX, nRY
		} else if nCX != nCY {
			return nil, newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
		} else {
			trendType, M, N = 3, nRX, nCY
		}
	} else {
		newX = getNewMatrix(nCY, nRY)
		nCX, nRX = nCY, nRY
		num := 1.0
		for i := 0; i < nRY; i++ {
			for j := 0; j < nCY; j++ {
				newX[j][i] = num
				num++
			}
		}
		trendType, M, N = 1, 1, cntY
	}
	return &trendGrowthMatrixInfo{
		trendType: trendType,
		nCX:       nCX,
		nCY:       nCY,
		nRX:       nRX,
		nRY:       nRY,
		M:         M,
		N:         N,
		mtxX:      newX,
		mtxY:      newY,
	}, newEmptyFormulaArg()
}

// calcPosition calculate position for matrix by given index.
func calcPosition(mtx [][]float64, idx int) (row, col int) {
	rowSize := len(mtx[0])
	col = idx
	if rowSize > 1 {
		col = idx / rowSize
	}
	row = idx - col*rowSize
	return
}

// getDouble returns float64 data type value in the matrix by given index.
func getDouble(mtx [][]float64, idx int) float64 {
	row, col := calcPosition(mtx, idx)
	return mtx[col][row]
}

// putDouble set a float64 data type value in the matrix by given index.
func putDouble(mtx [][]float64, idx int, val float64) {
	row, col := calcPosition(mtx, idx)
	mtx[col][row] = val
}

// calcMeanOverAll returns mean of the given matrix by over all element.
func calcMeanOverAll(mtx [][]float64, n int) float64 {
	var sum float64
	for i := 0; i < len(mtx); i++ {
		for j := 0; j < len(mtx[i]); j++ {
			sum += mtx[i][j]
		}
	}
	return sum / float64(n)
}

// calcSumProduct returns uses the matrices as vectors of length M over all
// element.
func calcSumProduct(mtxA, mtxB [][]float64, m int) float64 {
	sum := 0.0
	for i := 0; i < m; i++ {
		sum += getDouble(mtxA, i) * getDouble(mtxB, i)
	}
	return sum
}

// calcColumnMeans calculates means of the columns of matrix.
func calcColumnMeans(mtxX, mtxRes [][]float64, c, r int) {
	for i := 0; i < c; i++ {
		var sum float64
		for k := 0; k < r; k++ {
			sum += mtxX[i][k]
		}
		putDouble(mtxRes, i, sum/float64(r))
	}
}

// calcColumnsDelta calculates subtract of the columns of matrix.
func calcColumnsDelta(mtx, columnMeans [][]float64, c, r int) {
	for i := 0; i < c; i++ {
		for k := 0; k < r; k++ {
			mtx[i][k] = approxSub(mtx[i][k], getDouble(columnMeans, i))
		}
	}
}

// calcSign returns sign by given value, no mathematical signum, but used to
// switch between adding and subtracting.
func calcSign(val float64) float64 {
	if val > 0 {
		return 1
	}
	return -1
}

// calcColsMaximumNorm is a special version for use within QR
// decomposition. Maximum norm of column index c starting in row index r;
// matrix A has count n rows.
func calcColsMaximumNorm(mtxA [][]float64, c, r, n int) float64 {
	var norm float64
	for row := r; row < n; row++ {
		if norm < math.Abs(mtxA[c][row]) {
			norm = math.Abs(mtxA[c][row])
		}
	}
	return norm
}

// calcFastMult returns multiply n x m matrix A with m x l matrix B to n x l matrix R.
func calcFastMult(mtxA, mtxB, mtxR [][]float64, n, m, l int) {
	var sum float64
	for row := 0; row < n; row++ {
		for col := 0; col < l; col++ {
			sum = 0.0
			for k := 0; k < m; k++ {
				sum += mtxA[k][row] * mtxB[col][k]
			}
			mtxR[col][row] = sum
		}
	}
}

// calcRowsEuclideanNorm is a special version for use within QR
// decomposition. Euclidean norm of column index c starting in row index r;
// matrix a has count n rows.
func calcRowsEuclideanNorm(mtxA [][]float64, c, r, n int) float64 {
	var norm float64
	for row := r; row < n; row++ {
		norm += mtxA[c][row] * mtxA[c][row]
	}
	return math.Sqrt(norm)
}

// calcRowsSumProduct is a special version for use within QR decomposition.
// <A(a);B(b)> starting in row index r;
// a and b are indices of columns, matrices A and B have count n rows.
func calcRowsSumProduct(mtxA [][]float64, a int, mtxB [][]float64, b, r, n int) float64 {
	var result float64
	for row := r; row < n; row++ {
		result += mtxA[a][row] * mtxB[b][row]
	}
	return result
}

// calcSolveWithUpperRightTriangle solve for X in R*X=S using back substitution.
func calcSolveWithUpperRightTriangle(mtxA [][]float64, vecR []float64, mtxS [][]float64, k int, bIsTransposed bool) {
	var row int
	for rowp1 := k; rowp1 > 0; rowp1-- {
		row = rowp1 - 1
		sum := getDouble(mtxS, row)
		for col := rowp1; col < k; col++ {
			if bIsTransposed {
				sum -= mtxA[row][col] * getDouble(mtxS, col)
			} else {
				sum -= mtxA[col][row] * getDouble(mtxS, col)
			}
		}
		putDouble(mtxS, row, sum/vecR[row])
	}
}

// calcRowQRDecomposition calculates a QR decomposition with Householder
// reflection.
func calcRowQRDecomposition(mtxA [][]float64, vecR []float64, k, n int) bool {
	for col := 0; col < k; col++ {
		scale := calcColsMaximumNorm(mtxA, col, col, n)
		if scale == 0 {
			return false
		}
		for row := col; row < n; row++ {
			mtxA[col][row] = mtxA[col][row] / scale
		}
		euclid := calcRowsEuclideanNorm(mtxA, col, col, n)
		factor := 1.0 / euclid / (euclid + math.Abs(mtxA[col][col]))
		signum := calcSign(mtxA[col][col])
		mtxA[col][col] = mtxA[col][col] + signum*euclid
		vecR[col] = -signum * scale * euclid
		// apply Householder transformation to A
		for c := col + 1; c < k; c++ {
			sum := calcRowsSumProduct(mtxA, col, mtxA, c, col, n)
			for row := col; row < n; row++ {
				mtxA[c][row] = mtxA[c][row] - sum*factor*mtxA[col][row]
			}
		}
	}
	return true
}

// calcApplyColsHouseholderTransformation transposed matrices A and Y.
func calcApplyColsHouseholderTransformation(mtxA [][]float64, r int, mtxY [][]float64, n int) {
	denominator := calcColsSumProduct(mtxA, r, mtxA, r, r, n)
	numerator := calcColsSumProduct(mtxA, r, mtxY, 0, r, n)
	factor := 2 * (numerator / denominator)
	for col := r; col < n; col++ {
		putDouble(mtxY, col, getDouble(mtxY, col)-factor*mtxA[col][r])
	}
}

// calcRowMeans calculates means of the rows of matrix.
func calcRowMeans(mtxX, mtxRes [][]float64, c, r int) {
	for k := 0; k < r; k++ {
		var fSum float64
		for i := 0; i < c; i++ {
			fSum += mtxX[i][k]
		}
		mtxRes[k][0] = fSum / float64(c)
	}
}

// calcRowsDelta calculates subtract of the rows of matrix.
func calcRowsDelta(mtx, rowMeans [][]float64, c, r int) {
	for k := 0; k < r; k++ {
		for i := 0; i < c; i++ {
			mtx[i][k] = approxSub(mtx[i][k], rowMeans[k][0])
		}
	}
}

// calcColumnMaximumNorm returns maximum norm of row index R starting in col
// index C; matrix A has count N columns.
func calcColumnMaximumNorm(mtxA [][]float64, r, c, n int) float64 {
	var norm float64
	for col := c; col < n; col++ {
		if norm < math.Abs(mtxA[col][r]) {
			norm = math.Abs(mtxA[col][r])
		}
	}
	return norm
}

// calcColsEuclideanNorm returns euclidean norm of row index R starting in
// column index C; matrix A has count N columns.
func calcColsEuclideanNorm(mtxA [][]float64, r, c, n int) float64 {
	var norm float64
	for col := c; col < n; col++ {
		norm += (mtxA[col][r]) * (mtxA[col][r])
	}
	return math.Sqrt(norm)
}

// calcColsSumProduct returns sum product for given matrix.
func calcColsSumProduct(mtxA [][]float64, a int, mtxB [][]float64, b, c, n int) float64 {
	var result float64
	for col := c; col < n; col++ {
		result += mtxA[col][a] * mtxB[col][b]
	}
	return result
}

// calcColQRDecomposition same with transposed matrix A, N is count of
// columns, k count of rows.
func calcColQRDecomposition(mtxA [][]float64, vecR []float64, k, n int) bool {
	var sum float64
	for row := 0; row < k; row++ {
		// calculate vector u of the householder transformation
		scale := calcColumnMaximumNorm(mtxA, row, row, n)
		if scale == 0 {
			return false
		}
		for col := row; col < n; col++ {
			mtxA[col][row] = mtxA[col][row] / scale
		}
		euclid := calcColsEuclideanNorm(mtxA, row, row, n)
		factor := 1 / euclid / (euclid + math.Abs(mtxA[row][row]))
		signum := calcSign(mtxA[row][row])
		mtxA[row][row] = mtxA[row][row] + signum*euclid
		vecR[row] = -signum * scale * euclid
		// apply Householder transformation to A
		for r := row + 1; r < k; r++ {
			sum = calcColsSumProduct(mtxA, row, mtxA, r, row, n)
			for col := row; col < n; col++ {
				mtxA[col][r] = mtxA[col][r] - sum*factor*mtxA[col][row]
			}
		}
	}
	return true
}

// calcApplyRowsHouseholderTransformation applies a Householder transformation to a
// column vector Y with is given as Nx1 Matrix. The vector u, from which the
// Householder transformation is built, is the column part in matrix A, with
// column index c, starting with row index c. A is the result of the QR
// decomposition as obtained from calcRowQRDecomposition.
func calcApplyRowsHouseholderTransformation(mtxA [][]float64, c int, mtxY [][]float64, n int) {
	denominator := calcRowsSumProduct(mtxA, c, mtxA, c, c, n)
	numerator := calcRowsSumProduct(mtxA, c, mtxY, 0, c, n)
	factor := 2 * (numerator / denominator)
	for row := c; row < n; row++ {
		putDouble(mtxY, row, getDouble(mtxY, row)-factor*mtxA[c][row])
	}
}

// calcTrendGrowthSimpleRegression calculate simple regression for the calcTrendGrowth.
func calcTrendGrowthSimpleRegression(bConstant, bGrowth bool, mtxY, mtxX, newX, mtxRes [][]float64, meanY float64, N int) {
	var meanX float64
	if bConstant {
		meanX = calcMeanOverAll(mtxX, N)
		for i := 0; i < len(mtxX); i++ {
			for j := 0; j < len(mtxX[i]); j++ {
				mtxX[i][j] = approxSub(mtxX[i][j], meanX)
			}
		}
	}
	sumXY := calcSumProduct(mtxX, mtxY, N)
	sumX2 := calcSumProduct(mtxX, mtxX, N)
	slope := sumXY / sumX2
	var help float64
	var intercept float64
	if bConstant {
		intercept = meanY - slope*meanX
		for i := 0; i < len(mtxRes); i++ {
			for j := 0; j < len(mtxRes[i]); j++ {
				help = newX[i][j]*slope + intercept
				if bGrowth {
					mtxRes[i][j] = math.Exp(help)
				} else {
					mtxRes[i][j] = help
				}
			}
		}
	} else {
		for i := 0; i < len(mtxRes); i++ {
			for j := 0; j < len(mtxRes[i]); j++ {
				help = newX[i][j] * slope
				if bGrowth {
					mtxRes[i][j] = math.Exp(help)
				} else {
					mtxRes[i][j] = help
				}
			}
		}
	}
}

// calcTrendGrowthMultipleRegressionPart1 calculate multiple regression for the
// calcTrendGrowth.
func calcTrendGrowthMultipleRegressionPart1(bConstant, bGrowth bool, mtxY, mtxX, newX, mtxRes [][]float64, meanY float64, RXN, K, N int) {
	vecR := make([]float64, N)   // for QR decomposition
	means := getNewMatrix(K, 1)  // mean of each column
	slopes := getNewMatrix(1, K) // from b1 to bK
	if len(means) == 0 || len(slopes) == 0 {
		return
	}
	if bConstant {
		calcColumnMeans(mtxX, means, K, N)
		calcColumnsDelta(mtxX, means, K, N)
	}
	if !calcRowQRDecomposition(mtxX, vecR, K, N) {
		return
	}
	// Later on we will divide by elements of vecR, so make sure that they aren't zero.
	bIsSingular := false
	for row := 0; row < K && !bIsSingular; row++ {
		bIsSingular = bIsSingular || vecR[row] == 0
	}
	if bIsSingular {
		return
	}
	for col := 0; col < K; col++ {
		calcApplyRowsHouseholderTransformation(mtxX, col, mtxY, N)
	}
	for col := 0; col < K; col++ {
		putDouble(slopes, col, getDouble(mtxY, col))
	}
	calcSolveWithUpperRightTriangle(mtxX, vecR, slopes, K, false)
	// Fill result matrix
	calcFastMult(newX, slopes, mtxRes, RXN, K, 1)
	if bConstant {
		intercept := meanY - calcSumProduct(means, slopes, K)
		for row := 0; row < RXN; row++ {
			mtxRes[0][row] = mtxRes[0][row] + intercept
		}
	}
	if bGrowth {
		for i := 0; i < RXN; i++ {
			putDouble(mtxRes, i, math.Exp(getDouble(mtxRes, i)))
		}
	}
}

// calcTrendGrowthMultipleRegressionPart2 calculate multiple regression for the
// calcTrendGrowth.
func calcTrendGrowthMultipleRegressionPart2(bConstant, bGrowth bool, mtxY, mtxX, newX, mtxRes [][]float64, meanY float64, nCXN, K, N int) {
	vecR := make([]float64, N)   // for QR decomposition
	means := getNewMatrix(K, 1)  // mean of each row
	slopes := getNewMatrix(K, 1) // row from b1 to bK
	if len(means) == 0 || len(slopes) == 0 {
		return
	}
	if bConstant {
		calcRowMeans(mtxX, means, N, K)
		calcRowsDelta(mtxX, means, N, K)
	}
	if !calcColQRDecomposition(mtxX, vecR, K, N) {
		return
	}
	// later on we will divide by elements of vecR, so make sure that they aren't zero
	bIsSingular := false
	for row := 0; row < K && !bIsSingular; row++ {
		bIsSingular = bIsSingular || vecR[row] == 0
	}
	if bIsSingular {
		return
	}
	for row := 0; row < K; row++ {
		calcApplyColsHouseholderTransformation(mtxX, row, mtxY, N)
	}
	for col := 0; col < K; col++ {
		putDouble(slopes, col, getDouble(mtxY, col))
	}
	calcSolveWithUpperRightTriangle(mtxX, vecR, slopes, K, true)
	// fill result matrix
	calcFastMult(slopes, newX, mtxRes, 1, K, nCXN)
	if bConstant {
		fIntercept := meanY - calcSumProduct(means, slopes, K)
		for col := 0; col < nCXN; col++ {
			mtxRes[col][0] = mtxRes[col][0] + fIntercept
		}
	}
	if bGrowth {
		for i := 0; i < nCXN; i++ {
			putDouble(mtxRes, i, math.Exp(getDouble(mtxRes, i)))
		}
	}
}

// calcTrendGrowthRegression is a part of implementation of the calcTrendGrowth.
func calcTrendGrowthRegression(bConstant, bGrowth bool, trendType, nCXN, nRXN, K, N int, mtxY, mtxX, newX, mtxRes [][]float64) {
	if len(mtxRes) == 0 {
		return
	}
	var meanY float64
	if bConstant {
		copyX, copyY := matrixClone(mtxX), matrixClone(mtxY)
		mtxX, mtxY = copyX, copyY
		meanY = calcMeanOverAll(mtxY, N)
		for i := 0; i < len(mtxY); i++ {
			for j := 0; j < len(mtxY[i]); j++ {
				mtxY[i][j] = approxSub(mtxY[i][j], meanY)
			}
		}
	}
	switch trendType {
	case 1:
		calcTrendGrowthSimpleRegression(bConstant, bGrowth, mtxY, mtxX, newX, mtxRes, meanY, N)
	case 2:
		calcTrendGrowthMultipleRegressionPart1(bConstant, bGrowth, mtxY, mtxX, newX, mtxRes, meanY, nRXN, K, N)
	default:
		calcTrendGrowthMultipleRegressionPart2(bConstant, bGrowth, mtxY, mtxX, newX, mtxRes, meanY, nCXN, K, N)
	}
}

// calcTrendGrowth returns values along a predicted exponential trend.
func calcTrendGrowth(mtxY, mtxX, newX [][]float64, bConstant, bGrowth bool) ([][]float64, formulaArg) {
	getMatrixParams, errArg := prepareTrendGrowth(bGrowth, mtxX, mtxY)
	if errArg.Type != ArgEmpty {
		return nil, errArg
	}
	trendType := getMatrixParams.trendType
	nCX := getMatrixParams.nCX
	nRX := getMatrixParams.nRX
	K := getMatrixParams.M
	N := getMatrixParams.N
	mtxX = getMatrixParams.mtxX
	mtxY = getMatrixParams.mtxY
	// checking if data samples are enough
	if (bConstant && (N < K+1)) || (!bConstant && (N < K)) || (N < 1) || (K < 1) {
		return nil, errArg
	}
	// set the default newX if necessary
	nCXN, nRXN := nCX, nRX
	if len(newX) == 0 {
		newX = matrixClone(mtxX) // mtxX will be changed to X-meanX
	} else {
		nRXN, nCXN = len(newX[0]), len(newX)
		if (trendType == 2 && K != nCXN) || (trendType == 3 && K != nRXN) {
			return nil, errArg
		}
	}
	var mtxRes [][]float64
	switch trendType {
	case 1:
		mtxRes = getNewMatrix(nCXN, nRXN)
	case 2:
		mtxRes = getNewMatrix(1, nRXN)
	default:
		mtxRes = getNewMatrix(nCXN, 1)
	}
	calcTrendGrowthRegression(bConstant, bGrowth, trendType, nCXN, nRXN, K, N, mtxY, mtxX, newX, mtxRes)
	return mtxRes, errArg
}

// trendGrowth is an implementation of the formula functions GROWTH and TREND.
func (fn *formulaFuncs) trendGrowth(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	if argsList.Len() > 4 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 4 arguments", name))
	}
	var knowY, knowX, newX [][]float64
	var errArg formulaArg
	constArg := newBoolFormulaArg(true)
	knowY, errArg = newNumberMatrix(argsList.Front().Value.(formulaArg), false)
	if errArg.Type == ArgError {
		return errArg
	}
	if argsList.Len() > 1 {
		knowX, errArg = newNumberMatrix(argsList.Front().Next().Value.(formulaArg), false)
		if errArg.Type == ArgError {
			return errArg
		}
	}
	if argsList.Len() > 2 {
		newX, errArg = newNumberMatrix(argsList.Front().Next().Next().Value.(formulaArg), false)
		if errArg.Type == ArgError {
			return errArg
		}
	}
	if argsList.Len() > 3 {
		if constArg = argsList.Back().Value.(formulaArg).ToBool(); constArg.Type != ArgNumber {
			return constArg
		}
	}
	var mtxNewX [][]float64
	for i := 0; i < len(newX); i++ {
		for j := 0; j < len(newX[i]); j++ {
			for x := len(mtxNewX); x <= j; x++ {
				mtxNewX = append(mtxNewX, []float64{})
			}
			for k := len(mtxNewX[j]); k <= i; k++ {
				mtxNewX[j] = append(mtxNewX[j], 0)
			}
			mtxNewX[j][i] = newX[i][j]
		}
	}
	mtx, errArg := calcTrendGrowth(knowY, knowX, mtxNewX, constArg.Number == 1, name == "GROWTH")
	if errArg.Type != ArgEmpty {
		return errArg
	}
	return newMatrixFormulaArg(newFormulaArgMatrix(mtx))
}

// GROWTH function calculates the exponential growth curve through a given set
// of y-values and (optionally), one or more sets of x-values. The function
// then extends the curve to calculate additional y-values for a further
// supplied set of new x-values. The syntax of the function is:
//
//	GROWTH(known_y's,[known_x's],[new_x's],[const])
func (fn *formulaFuncs) GROWTH(argsList *list.List) formulaArg {
	return fn.trendGrowth("GROWTH", argsList)
}

// HARMEAN function calculates the harmonic mean of a supplied set of values.
// The syntax of the function is:
//
//	HARMEAN(number1,[number2],...)
func (fn *formulaFuncs) HARMEAN(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HARMEAN requires at least 1 argument")
	}
	if min := fn.MIN(argsList); min.Number < 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	number, val, cnt := 0.0, 0.0, 0.0
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			num := arg.ToNumber()
			if num.Type != ArgNumber {
				continue
			}
			number = num.Number
		case ArgNumber:
			number = arg.Number
		}
		if number <= 0 {
			return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
		}
		val += 1 / number
		cnt++
	}
	return newNumberFormulaArg(1 / (val / cnt))
}

// checkHYPGEOMDISTArgs checking arguments for the formula function HYPGEOMDIST
// and HYPGEOM.DIST.
func checkHYPGEOMDISTArgs(sampleS, numberSample, populationS, numberPop formulaArg) bool {
	return sampleS.Number < 0 ||
		sampleS.Number > math.Min(numberSample.Number, populationS.Number) ||
		sampleS.Number < math.Max(0, numberSample.Number-numberPop.Number+populationS.Number) ||
		numberSample.Number <= 0 ||
		numberSample.Number > numberPop.Number ||
		populationS.Number <= 0 ||
		populationS.Number > numberPop.Number ||
		numberPop.Number <= 0
}

// prepareHYPGEOMDISTArgs prepare arguments for the formula function
// HYPGEOMDIST and HYPGEOM.DIST.
func (fn *formulaFuncs) prepareHYPGEOMDISTArgs(name string, argsList *list.List) formulaArg {
	if name == "HYPGEOMDIST" && argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "HYPGEOMDIST requires 4 numeric arguments")
	}
	if name == "HYPGEOM.DIST" && argsList.Len() != 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "HYPGEOM.DIST requires 5 arguments")
	}
	var sampleS, numberSample, populationS, numberPop, cumulative formulaArg
	if sampleS = argsList.Front().Value.(formulaArg).ToNumber(); sampleS.Type != ArgNumber {
		return sampleS
	}
	if numberSample = argsList.Front().Next().Value.(formulaArg).ToNumber(); numberSample.Type != ArgNumber {
		return numberSample
	}
	if populationS = argsList.Front().Next().Next().Value.(formulaArg).ToNumber(); populationS.Type != ArgNumber {
		return populationS
	}
	if numberPop = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); numberPop.Type != ArgNumber {
		return numberPop
	}
	if checkHYPGEOMDISTArgs(sampleS, numberSample, populationS, numberPop) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if name == "HYPGEOM.DIST" {
		if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type != ArgNumber {
			return cumulative
		}
	}
	return newListFormulaArg([]formulaArg{sampleS, numberSample, populationS, numberPop, cumulative})
}

// HYPGEOMdotDIST function returns the value of the hypergeometric distribution
// for a specified number of successes from a population sample. The function
// can calculate the cumulative distribution or the probability density
// function. The syntax of the function is:
//
//	HYPGEOM.DIST(sample_s,number_sample,population_s,number_pop,cumulative)
func (fn *formulaFuncs) HYPGEOMdotDIST(argsList *list.List) formulaArg {
	args := fn.prepareHYPGEOMDISTArgs("HYPGEOM.DIST", argsList)
	if args.Type != ArgList {
		return args
	}
	sampleS, numberSample, populationS, numberPop, cumulative := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4]
	if cumulative.Number == 1 {
		var res float64
		for i := 0; i <= int(sampleS.Number); i++ {
			res += binomCoeff(populationS.Number, float64(i)) *
				binomCoeff(numberPop.Number-populationS.Number, numberSample.Number-float64(i)) /
				binomCoeff(numberPop.Number, numberSample.Number)
		}
		return newNumberFormulaArg(res)
	}
	return newNumberFormulaArg(binomCoeff(populationS.Number, sampleS.Number) *
		binomCoeff(numberPop.Number-populationS.Number, numberSample.Number-sampleS.Number) /
		binomCoeff(numberPop.Number, numberSample.Number))
}

// HYPGEOMDIST function returns the value of the hypergeometric distribution
// for a given number of successes from a sample of a population. The syntax
// of the function is:
//
//	HYPGEOMDIST(sample_s,number_sample,population_s,number_pop)
func (fn *formulaFuncs) HYPGEOMDIST(argsList *list.List) formulaArg {
	args := fn.prepareHYPGEOMDISTArgs("HYPGEOMDIST", argsList)
	if args.Type != ArgList {
		return args
	}
	sampleS, numberSample, populationS, numberPop := args.List[0], args.List[1], args.List[2], args.List[3]
	return newNumberFormulaArg(binomCoeff(populationS.Number, sampleS.Number) *
		binomCoeff(numberPop.Number-populationS.Number, numberSample.Number-sampleS.Number) /
		binomCoeff(numberPop.Number, numberSample.Number))
}

// INTERCEPT function calculates the intercept (the value at the intersection
// of the y axis) of the linear regression line through a supplied set of x-
// and y- values. The syntax of the function is:
//
//	INTERCEPT(known_y's,known_x's)
func (fn *formulaFuncs) INTERCEPT(argsList *list.List) formulaArg {
	return fn.pearsonProduct("INTERCEPT", 2, argsList)
}

// KURT function calculates the kurtosis of a supplied set of values. The
// syntax of the function is:
//
//	KURT(number1,[number2],...)
func (fn *formulaFuncs) KURT(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "KURT requires at least 1 argument")
	}
	mean, stdev := fn.AVERAGE(argsList), fn.STDEV(argsList)
	if stdev.Number > 0 {
		count, summer := 0.0, 0.0
		for arg := argsList.Front(); arg != nil; arg = arg.Next() {
			token := arg.Value.(formulaArg)
			switch token.Type {
			case ArgString, ArgNumber:
				num := token.ToNumber()
				if num.Type == ArgError {
					continue
				}
				summer += math.Pow((num.Number-mean.Number)/stdev.Number, 4)
				count++
			case ArgList, ArgMatrix:
				for _, row := range token.ToList() {
					if row.Type == ArgNumber || row.Type == ArgString {
						num := row.ToNumber()
						if num.Type == ArgError {
							continue
						}
						summer += math.Pow((num.Number-mean.Number)/stdev.Number, 4)
						count++
					}
				}
			}
		}
		if count > 3 {
			return newNumberFormulaArg(summer*(count*(count+1)/((count-1)*(count-2)*(count-3))) - (3 * math.Pow(count-1, 2) / ((count - 2) * (count - 3))))
		}
	}
	return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
}

// EXPONdotDIST function returns the value of the exponential distribution for
// a give value of x. The user can specify whether the probability density
// function or the cumulative distribution function is used. The syntax of the
// Expondist function is:
//
//	EXPON.DIST(x,lambda,cumulative)
func (fn *formulaFuncs) EXPONdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "EXPON.DIST requires 3 arguments")
	}
	return fn.EXPONDIST(argsList)
}

// EXPONDIST function returns the value of the exponential distribution for a
// give value of x. The user can specify whether the probability density
// function or the cumulative distribution function is used. The syntax of the
// Expondist function is:
//
//	EXPONDIST(x,lambda,cumulative)
func (fn *formulaFuncs) EXPONDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "EXPONDIST requires 3 arguments")
	}
	var x, lambda, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if lambda = argsList.Front().Next().Value.(formulaArg).ToNumber(); lambda.Type != ArgNumber {
		return lambda
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if x.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if lambda.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(1 - math.Exp(-lambda.Number*x.Number))
	}
	return newNumberFormulaArg(lambda.Number * math.Exp(-lambda.Number*x.Number))
}

// FdotDIST function calculates the Probability Density Function or the
// Cumulative Distribution Function for the F Distribution. This function is
// frequently used to measure the degree of diversity between two data
// sets. The syntax of the function is:
//
//	F.DIST(x,deg_freedom1,deg_freedom2,cumulative)
func (fn *formulaFuncs) FdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "F.DIST requires 4 arguments")
	}
	var x, deg1, deg2, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if deg1 = argsList.Front().Next().Value.(formulaArg).ToNumber(); deg1.Type != ArgNumber {
		return deg1
	}
	if deg2 = argsList.Front().Next().Next().Value.(formulaArg).ToNumber(); deg2.Type != ArgNumber {
		return deg2
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if x.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	maxDeg := math.Pow10(10)
	if deg1.Number < 1 || deg1.Number >= maxDeg {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if deg2.Number < 1 || deg2.Number >= maxDeg {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(1 - getBetaDist(deg2.Number/(deg2.Number+deg1.Number*x.Number), deg2.Number/2, deg1.Number/2))
	}
	return newNumberFormulaArg(math.Gamma((deg2.Number+deg1.Number)/2) / (math.Gamma(deg1.Number/2) * math.Gamma(deg2.Number/2)) * math.Pow(deg1.Number/deg2.Number, deg1.Number/2) * (math.Pow(x.Number, (deg1.Number-2)/2) / math.Pow(1+(deg1.Number/deg2.Number)*x.Number, (deg1.Number+deg2.Number)/2)))
}

// FDIST function calculates the (right-tailed) F Probability Distribution,
// which measures the degree of diversity between two data sets. The syntax
// of the function is:
//
//	FDIST(x,deg_freedom1,deg_freedom2)
func (fn *formulaFuncs) FDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "FDIST requires 3 arguments")
	}
	var x, deg1, deg2 formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if deg1 = argsList.Front().Next().Value.(formulaArg).ToNumber(); deg1.Type != ArgNumber {
		return deg1
	}
	if deg2 = argsList.Back().Value.(formulaArg).ToNumber(); deg2.Type != ArgNumber {
		return deg2
	}
	if x.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	maxDeg := math.Pow10(10)
	if deg1.Number < 1 || deg1.Number >= maxDeg {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if deg2.Number < 1 || deg2.Number >= maxDeg {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg(deg1.Number * x.Number / (deg1.Number*x.Number + deg2.Number)))
	args.PushBack(newNumberFormulaArg(0.5 * deg1.Number))
	args.PushBack(newNumberFormulaArg(0.5 * deg2.Number))
	args.PushBack(newNumberFormulaArg(0))
	args.PushBack(newNumberFormulaArg(1))
	return newNumberFormulaArg(1 - fn.BETADIST(args).Number)
}

// FdotDISTdotRT function calculates the (right-tailed) F Probability
// Distribution, which measures the degree of diversity between two data sets.
// The syntax of the function is:
//
//	F.DIST.RT(x,deg_freedom1,deg_freedom2)
func (fn *formulaFuncs) FdotDISTdotRT(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "F.DIST.RT requires 3 arguments")
	}
	return fn.FDIST(argsList)
}

// prepareFinvArgs checking and prepare arguments for the formula functions
// F.INV, F.INV.RT and FINV.
func (fn *formulaFuncs) prepareFinvArgs(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 3 arguments", name))
	}
	var probability, d1, d2 formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if d1 = argsList.Front().Next().Value.(formulaArg).ToNumber(); d1.Type != ArgNumber {
		return d1
	}
	if d2 = argsList.Back().Value.(formulaArg).ToNumber(); d2.Type != ArgNumber {
		return d2
	}
	if probability.Number <= 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if d1.Number < 1 || d1.Number >= math.Pow10(10) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if d2.Number < 1 || d2.Number >= math.Pow10(10) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newListFormulaArg([]formulaArg{probability, d1, d2})
}

// FdotINV function calculates the inverse of the Cumulative F Distribution
// for a supplied probability. The syntax of the F.Inv function is:
//
//	F.INV(probability,deg_freedom1,deg_freedom2)
func (fn *formulaFuncs) FdotINV(argsList *list.List) formulaArg {
	args := fn.prepareFinvArgs("F.INV", argsList)
	if args.Type != ArgList {
		return args
	}
	probability, d1, d2 := args.List[0], args.List[1], args.List[2]
	return newNumberFormulaArg((1/calcBetainv(1-probability.Number, d2.Number/2, d1.Number/2, 0, 1) - 1) * (d2.Number / d1.Number))
}

// FdotINVdotRT function calculates the inverse of the (right-tailed) F
// Probability Distribution for a supplied probability. The syntax of the
// function is:
//
//	F.INV.RT(probability,deg_freedom1,deg_freedom2)
func (fn *formulaFuncs) FdotINVdotRT(argsList *list.List) formulaArg {
	args := fn.prepareFinvArgs("F.INV.RT", argsList)
	if args.Type != ArgList {
		return args
	}
	probability, d1, d2 := args.List[0], args.List[1], args.List[2]
	return newNumberFormulaArg((1/calcBetainv(1-(1-probability.Number), d2.Number/2, d1.Number/2, 0, 1) - 1) * (d2.Number / d1.Number))
}

// FINV function calculates the inverse of the (right-tailed) F Probability
// Distribution for a supplied probability. The syntax of the function is:
//
//	FINV(probability,deg_freedom1,deg_freedom2)
func (fn *formulaFuncs) FINV(argsList *list.List) formulaArg {
	args := fn.prepareFinvArgs("FINV", argsList)
	if args.Type != ArgList {
		return args
	}
	probability, d1, d2 := args.List[0], args.List[1], args.List[2]
	return newNumberFormulaArg((1/calcBetainv(1-(1-probability.Number), d2.Number/2, d1.Number/2, 0, 1) - 1) * (d2.Number / d1.Number))
}

// FdotTEST function returns the F-Test for two supplied arrays. I.e. the
// function returns the two-tailed probability that the variances in the two
// supplied arrays are not significantly different. The syntax of the Ftest
// function is:
//
//	F.TEST(array1,array2)
func (fn *formulaFuncs) FdotTEST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "F.TEST requires 2 arguments")
	}
	array1 := argsList.Front().Value.(formulaArg)
	array2 := argsList.Back().Value.(formulaArg)
	left, right := array1.ToList(), array2.ToList()
	collectMatrix := func(args []formulaArg) (n, accu float64) {
		var p, sum float64
		for _, arg := range args {
			if num := arg.ToNumber(); num.Type == ArgNumber {
				x := num.Number - p
				y := x / (n + 1)
				p += y
				accu += n * x * y
				n++
				sum += num.Number
			}
		}
		return
	}
	nums, accu := collectMatrix(left)
	f3 := nums - 1
	if nums == 1 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	f1 := accu / (nums - 1)
	if f1 == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	nums, accu = collectMatrix(right)
	f4 := nums - 1
	if nums == 1 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	f2 := accu / (nums - 1)
	if f2 == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg(f1 / f2))
	args.PushBack(newNumberFormulaArg(f3))
	args.PushBack(newNumberFormulaArg(f4))
	probability := (1 - fn.FDIST(args).Number) * 2
	if probability > 1 {
		probability = 2 - probability
	}
	return newNumberFormulaArg(probability)
}

// FTEST function returns the F-Test for two supplied arrays. I.e. the function
// returns the two-tailed probability that the variances in the two supplied
// arrays are not significantly different. The syntax of the Ftest function
// is:
//
//	FTEST(array1,array2)
func (fn *formulaFuncs) FTEST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "FTEST requires 2 arguments")
	}
	return fn.FdotTEST(argsList)
}

// LOGINV function calculates the inverse of the Cumulative Log-Normal
// Distribution Function of x, for a supplied probability. The syntax of the
// function is:
//
//	LOGINV(probability,mean,standard_dev)
func (fn *formulaFuncs) LOGINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOGINV requires 3 arguments")
	}
	var probability, mean, stdDev formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if stdDev = argsList.Back().Value.(formulaArg).ToNumber(); stdDev.Type != ArgNumber {
		return stdDev
	}
	if probability.Number <= 0 || probability.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if stdDev.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New()
	args.PushBack(probability)
	args.PushBack(newNumberFormulaArg(0))
	args.PushBack(newNumberFormulaArg(1))
	norminv := fn.NORMINV(args)
	return newNumberFormulaArg(math.Exp(mean.Number + stdDev.Number*norminv.Number))
}

// LOGNORMdotINV function calculates the inverse of the Cumulative Log-Normal
// Distribution Function of x, for a supplied probability. The syntax of the
// function is:
//
//	LOGNORM.INV(probability,mean,standard_dev)
func (fn *formulaFuncs) LOGNORMdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOGNORM.INV requires 3 arguments")
	}
	return fn.LOGINV(argsList)
}

// LOGNORMdotDIST function calculates the Log-Normal Probability Density
// Function or the Cumulative Log-Normal Distribution Function for a supplied
// value of x. The syntax of the function is:
//
//	LOGNORM.DIST(x,mean,standard_dev,cumulative)
func (fn *formulaFuncs) LOGNORMdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOGNORM.DIST requires 4 arguments")
	}
	var x, mean, stdDev, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if stdDev = argsList.Back().Prev().Value.(formulaArg).ToNumber(); stdDev.Type != ArgNumber {
		return stdDev
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if x.Number <= 0 || stdDev.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 1 {
		args := list.New()
		args.PushBack(newNumberFormulaArg((math.Log(x.Number) - mean.Number) / stdDev.Number))
		args.PushBack(newNumberFormulaArg(0))
		args.PushBack(newNumberFormulaArg(1))
		args.PushBack(cumulative)
		return fn.NORMDIST(args)
	}
	return newNumberFormulaArg((1 / (math.Sqrt(2*math.Pi) * stdDev.Number * x.Number)) *
		math.Exp(0-(math.Pow(math.Log(x.Number)-mean.Number, 2)/(2*math.Pow(stdDev.Number, 2)))))
}

// LOGNORMDIST function calculates the Cumulative Log-Normal Distribution
// Function at a supplied value of x. The syntax of the function is:
//
//	LOGNORMDIST(x,mean,standard_dev)
func (fn *formulaFuncs) LOGNORMDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOGNORMDIST requires 3 arguments")
	}
	var x, mean, stdDev formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if stdDev = argsList.Back().Value.(formulaArg).ToNumber(); stdDev.Type != ArgNumber {
		return stdDev
	}
	if x.Number <= 0 || stdDev.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg((math.Log(x.Number) - mean.Number) / stdDev.Number))
	return fn.NORMSDIST(args)
}

// MODE function returns the statistical mode (the most frequently occurring
// value) of a list of supplied numbers. If there are 2 or more most
// frequently occurring values in the supplied data, the function returns the
// lowest of these values The syntax of the function is:
//
//	MODE(number1,[number2],...)
func (fn *formulaFuncs) MODE(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MODE requires at least 1 argument")
	}
	var values []float64
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		cells := arg.Value.(formulaArg)
		if cells.Type != ArgMatrix && cells.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		for _, cell := range cells.ToList() {
			if cell.Type == ArgNumber {
				values = append(values, cell.Number)
			}
		}
	}
	sort.Float64s(values)
	cnt := len(values)
	var count, modeCnt int
	var mode float64
	for i := 0; i < cnt; i++ {
		count = 0
		for j := 0; j < cnt; j++ {
			if j != i && values[j] == values[i] {
				count++
			}
		}
		if count > modeCnt {
			modeCnt = count
			mode = values[i]
		}
	}
	if modeCnt == 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(mode)
}

// MODEdotMULT function returns a vertical array of the statistical modes
// (the most frequently occurring values) within a list of supplied numbers.
// The syntax of the function is:
//
//	MODE.MULT(number1,[number2],...)
func (fn *formulaFuncs) MODEdotMULT(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MODE.MULT requires at least 1 argument")
	}
	var values []float64
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		cells := arg.Value.(formulaArg)
		if cells.Type != ArgMatrix && cells.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		for _, cell := range cells.ToList() {
			if cell.Type == ArgNumber {
				values = append(values, cell.Number)
			}
		}
	}
	sort.Float64s(values)
	cnt := len(values)
	var count, modeCnt int
	var mtx [][]formulaArg
	for i := 0; i < cnt; i++ {
		count = 0
		for j := i + 1; j < cnt; j++ {
			if values[i] == values[j] {
				count++
			}
		}
		if count > modeCnt {
			modeCnt = count
			mtx = [][]formulaArg{}
			mtx = append(mtx, []formulaArg{newNumberFormulaArg(values[i])})
		} else if count == modeCnt {
			mtx = append(mtx, []formulaArg{newNumberFormulaArg(values[i])})
		}
	}
	if modeCnt == 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newMatrixFormulaArg(mtx)
}

// MODEdotSNGL function returns the statistical mode (the most frequently
// occurring value) within a list of supplied numbers. If there are 2 or more
// most frequently occurring values in the supplied data, the function returns
// the lowest of these values. The syntax of the function is:
//
//	MODE.SNGL(number1,[number2],...)
func (fn *formulaFuncs) MODEdotSNGL(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MODE.SNGL requires at least 1 argument")
	}
	return fn.MODE(argsList)
}

// NEGBINOMdotDIST function calculates the probability mass function or the
// cumulative distribution function for the Negative Binomial Distribution.
// This gives the probability that there will be a given number of failures
// before a required number of successes is achieved. The syntax of the
// function is:
//
//	NEGBINOM.DIST(number_f,number_s,probability_s,cumulative)
func (fn *formulaFuncs) NEGBINOMdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "NEGBINOM.DIST requires 4 arguments")
	}
	var f, s, probability, cumulative formulaArg
	if f = argsList.Front().Value.(formulaArg).ToNumber(); f.Type != ArgNumber {
		return f
	}
	if s = argsList.Front().Next().Value.(formulaArg).ToNumber(); s.Type != ArgNumber {
		return s
	}
	if probability = argsList.Front().Next().Next().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type != ArgNumber {
		return cumulative
	}
	if f.Number < 0 || s.Number < 1 || probability.Number < 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(1 - getBetaDist(1-probability.Number, f.Number+1, s.Number))
	}
	return newNumberFormulaArg(binomCoeff(f.Number+s.Number-1, s.Number-1) * math.Pow(probability.Number, s.Number) * math.Pow(1-probability.Number, f.Number))
}

// NEGBINOMDIST function calculates the Negative Binomial Distribution for a
// given set of parameters. This gives the probability that there will be a
// specified number of failures before a required number of successes is
// achieved. The syntax of the function is:
//
//	NEGBINOMDIST(number_f,number_s,probability_s)
func (fn *formulaFuncs) NEGBINOMDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "NEGBINOMDIST requires 3 arguments")
	}
	var f, s, probability formulaArg
	if f = argsList.Front().Value.(formulaArg).ToNumber(); f.Type != ArgNumber {
		return f
	}
	if s = argsList.Front().Next().Value.(formulaArg).ToNumber(); s.Type != ArgNumber {
		return s
	}
	if probability = argsList.Back().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if f.Number < 0 || s.Number < 1 || probability.Number < 0 || probability.Number > 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(binomCoeff(f.Number+s.Number-1, s.Number-1) * math.Pow(probability.Number, s.Number) * math.Pow(1-probability.Number, f.Number))
}

// NORMdotDIST function calculates the Normal Probability Density Function or
// the Cumulative Normal Distribution. Function for a supplied set of
// parameters. The syntax of the function is:
//
//	NORM.DIST(x,mean,standard_dev,cumulative)
func (fn *formulaFuncs) NORMdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORM.DIST requires 4 arguments")
	}
	return fn.NORMDIST(argsList)
}

// NORMDIST function calculates the Normal Probability Density Function or the
// Cumulative Normal Distribution. Function for a supplied set of parameters.
// The syntax of the function is:
//
//	NORMDIST(x,mean,standard_dev,cumulative)
func (fn *formulaFuncs) NORMDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORMDIST requires 4 arguments")
	}
	var x, mean, stdDev, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if stdDev = argsList.Back().Prev().Value.(formulaArg).ToNumber(); stdDev.Type != ArgNumber {
		return stdDev
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type == ArgError {
		return cumulative
	}
	if stdDev.Number < 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if cumulative.Number == 1 {
		return newNumberFormulaArg(0.5 * (1 + math.Erf((x.Number-mean.Number)/(stdDev.Number*math.Sqrt(2)))))
	}
	return newNumberFormulaArg((1 / (math.Sqrt(2*math.Pi) * stdDev.Number)) * math.Exp(0-(math.Pow(x.Number-mean.Number, 2)/(2*(stdDev.Number*stdDev.Number)))))
}

// NORMdotINV function calculates the inverse of the Cumulative Normal
// Distribution Function for a supplied value of x, and a supplied
// distribution mean & standard deviation. The syntax of the function is:
//
//	NORM.INV(probability,mean,standard_dev)
func (fn *formulaFuncs) NORMdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORM.INV requires 3 arguments")
	}
	return fn.NORMINV(argsList)
}

// NORMINV function calculates the inverse of the Cumulative Normal
// Distribution Function for a supplied value of x, and a supplied
// distribution mean & standard deviation. The syntax of the function is:
//
//	NORMINV(probability,mean,standard_dev)
func (fn *formulaFuncs) NORMINV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORMINV requires 3 arguments")
	}
	var prob, mean, stdDev formulaArg
	if prob = argsList.Front().Value.(formulaArg).ToNumber(); prob.Type != ArgNumber {
		return prob
	}
	if mean = argsList.Front().Next().Value.(formulaArg).ToNumber(); mean.Type != ArgNumber {
		return mean
	}
	if stdDev = argsList.Back().Value.(formulaArg).ToNumber(); stdDev.Type != ArgNumber {
		return stdDev
	}
	if prob.Number < 0 || prob.Number > 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if stdDev.Number < 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	inv, err := norminv(prob.Number)
	if err != nil {
		return newErrorFormulaArg(err.Error(), err.Error())
	}
	return newNumberFormulaArg(inv*stdDev.Number + mean.Number)
}

// NORMdotSdotDIST function calculates the Standard Normal Cumulative
// Distribution Function for a supplied value. The syntax of the function
// is:
//
//	NORM.S.DIST(z)
func (fn *formulaFuncs) NORMdotSdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORM.S.DIST requires 2 numeric arguments")
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(formulaArg{Type: ArgNumber, Number: 0})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1})
	args.PushBack(argsList.Back().Value.(formulaArg))
	return fn.NORMDIST(args)
}

// NORMSDIST function calculates the Standard Normal Cumulative Distribution
// Function for a supplied value. The syntax of the function is:
//
//	NORMSDIST(z)
func (fn *formulaFuncs) NORMSDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORMSDIST requires 1 numeric argument")
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(formulaArg{Type: ArgNumber, Number: 0})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1, Boolean: true})
	return fn.NORMDIST(args)
}

// NORMSINV function calculates the inverse of the Standard Normal Cumulative
// Distribution Function for a supplied probability value. The syntax of the
// function is:
//
//	NORMSINV(probability)
func (fn *formulaFuncs) NORMSINV(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORMSINV requires 1 numeric argument")
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(formulaArg{Type: ArgNumber, Number: 0})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1})
	return fn.NORMINV(args)
}

// NORMdotSdotINV function calculates the inverse of the Standard Normal
// Cumulative Distribution Function for a supplied probability value. The
// syntax of the function is:
//
//	NORM.S.INV(probability)
func (fn *formulaFuncs) NORMdotSdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "NORM.S.INV requires 1 numeric argument")
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(formulaArg{Type: ArgNumber, Number: 0})
	args.PushBack(formulaArg{Type: ArgNumber, Number: 1})
	return fn.NORMINV(args)
}

// norminv returns the inverse of the normal cumulative distribution for the
// specified value.
func norminv(p float64) (float64, error) {
	a := map[int]float64{
		1: -3.969683028665376e+01, 2: 2.209460984245205e+02, 3: -2.759285104469687e+02,
		4: 1.383577518672690e+02, 5: -3.066479806614716e+01, 6: 2.506628277459239e+00,
	}
	b := map[int]float64{
		1: -5.447609879822406e+01, 2: 1.615858368580409e+02, 3: -1.556989798598866e+02,
		4: 6.680131188771972e+01, 5: -1.328068155288572e+01,
	}
	c := map[int]float64{
		1: -7.784894002430293e-03, 2: -3.223964580411365e-01, 3: -2.400758277161838e+00,
		4: -2.549732539343734e+00, 5: 4.374664141464968e+00, 6: 2.938163982698783e+00,
	}
	d := map[int]float64{
		1: 7.784695709041462e-03, 2: 3.224671290700398e-01, 3: 2.445134137142996e+00,
		4: 3.754408661907416e+00,
	}
	pLow := 0.02425   // Use lower region approx. below this
	pHigh := 1 - pLow // Use upper region approx. above this
	if 0 < p && p < pLow {
		// Rational approximation for lower region.
		q := math.Sqrt(-2 * math.Log(p))
		return (((((c[1]*q+c[2])*q+c[3])*q+c[4])*q+c[5])*q + c[6]) /
			((((d[1]*q+d[2])*q+d[3])*q+d[4])*q + 1), nil
	} else if pLow <= p && p <= pHigh {
		// Rational approximation for central region.
		q := p - 0.5
		r := q * q
		f1 := ((((a[1]*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * r
		f2 := (b[1]*r + b[2]) * r
		f3 := ((math.Nextafter(f2, f2)+b[3])*r + b[4]) * r
		f4 := (math.Nextafter(f3, f3) + b[5]) * r
		return (math.Nextafter(f1, f1) + a[6]) * q /
			(math.Nextafter(f4, f4) + 1), nil
	} else if pHigh < p && p < 1 {
		// Rational approximation for upper region.
		q := math.Sqrt(-2 * math.Log(1-p))
		return -(((((c[1]*q+c[2])*q+c[3])*q+c[4])*q+c[5])*q + c[6]) /
			((((d[1]*q+d[2])*q+d[3])*q+d[4])*q + 1), nil
	}
	return 0, errors.New(formulaErrorNUM)
}

// kth is an implementation of the formula functions LARGE and SMALL.
func (fn *formulaFuncs) kth(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 arguments", name))
	}
	array := argsList.Front().Value.(formulaArg).ToList()
	argK := argsList.Back().Value.(formulaArg).ToNumber()
	if argK.Type != ArgNumber {
		return argK
	}
	k := int(argK.Number)
	if k < 1 {
		return newErrorFormulaArg(formulaErrorNUM, "k should be > 0")
	}
	var data []float64
	for _, arg := range array {
		if arg.Type == ArgNumber {
			data = append(data, arg.Number)
		}
	}
	if len(data) < k {
		return newErrorFormulaArg(formulaErrorNUM, "k should be <= length of array")
	}
	sort.Float64s(data)
	if name == "LARGE" {
		return newNumberFormulaArg(data[len(data)-k])
	}
	return newNumberFormulaArg(data[k-1])
}

// LARGE function returns the k'th largest value from an array of numeric
// values. The syntax of the function is:
//
//	LARGE(array,k)
func (fn *formulaFuncs) LARGE(argsList *list.List) formulaArg {
	return fn.kth("LARGE", argsList)
}

// MAX function returns the largest value from a supplied set of numeric
// values. The syntax of the function is:
//
//	MAX(number1,[number2],...)
func (fn *formulaFuncs) MAX(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "MAX requires at least 1 argument")
	}
	return fn.max(false, argsList)
}

// MAXA function returns the largest value from a supplied set of numeric
// values, while counting text and the logical value FALSE as the value 0 and
// counting the logical value TRUE as the value 1. The syntax of the function
// is:
//
//	MAXA(number1,[number2],...)
func (fn *formulaFuncs) MAXA(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "MAXA requires at least 1 argument")
	}
	return fn.max(true, argsList)
}

// MAXIFS function returns the maximum value from a subset of values that are
// specified according to one or more criteria. The syntax of the function
// is:
//
//	MAXIFS(max_range,criteria_range1,criteria1,[criteria_range2,criteria2],...)
func (fn *formulaFuncs) MAXIFS(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "MAXIFS requires at least 3 arguments")
	}
	if argsList.Len()%2 != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var args []formulaArg
	max, maxRange := -math.MaxFloat64, argsList.Front().Value.(formulaArg).Matrix
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	for _, ref := range formulaIfsMatch(args) {
		if num := maxRange[ref.Row][ref.Col].ToNumber(); num.Type == ArgNumber && max < num.Number {
			max = num.Number
		}
	}
	if max == -math.MaxFloat64 {
		max = 0
	}
	return newNumberFormulaArg(max)
}

// calcListMatrixMax is part of the implementation max.
func calcListMatrixMax(maxa bool, max float64, arg formulaArg) float64 {
	for _, cell := range arg.ToList() {
		if cell.Type == ArgNumber && cell.Number > max {
			if maxa && cell.Boolean || !cell.Boolean {
				max = cell.Number
			}
		}
	}
	return max
}

// max is an implementation of the formula functions MAX and MAXA.
func (fn *formulaFuncs) max(maxa bool, argsList *list.List) formulaArg {
	max := -math.MaxFloat64
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			if !maxa && (arg.Value() == "TRUE" || arg.Value() == "FALSE") {
				continue
			} else {
				num := arg.ToBool()
				if num.Type == ArgNumber && num.Number > max {
					max = num.Number
					continue
				}
			}
			num := arg.ToNumber()
			if num.Type != ArgError && num.Number > max {
				max = num.Number
			}
		case ArgNumber:
			if arg.Number > max {
				max = arg.Number
			}
		case ArgList, ArgMatrix:
			max = calcListMatrixMax(maxa, max, arg)
		case ArgError:
			return arg
		}
	}
	if max == -math.MaxFloat64 {
		max = 0
	}
	return newNumberFormulaArg(max)
}

// MEDIAN function returns the statistical median (the middle value) of a list
// of supplied numbers. The syntax of the function is:
//
//	MEDIAN(number1,[number2],...)
func (fn *formulaFuncs) MEDIAN(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "MEDIAN requires at least 1 argument")
	}
	var values []float64
	var median float64
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			value := arg.ToNumber()
			if value.Type != ArgNumber {
				return value
			}
			values = append(values, value.Number)
		case ArgNumber:
			values = append(values, arg.Number)
		case ArgMatrix:
			for _, row := range arg.Matrix {
				for _, cell := range row {
					if cell.Type == ArgNumber {
						values = append(values, cell.Number)
					}
				}
			}
		}
	}
	if len(values) == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	sort.Float64s(values)
	if len(values)%2 == 0 {
		median = (values[len(values)/2-1] + values[len(values)/2]) / 2
	} else {
		median = values[len(values)/2]
	}
	return newNumberFormulaArg(median)
}

// MIN function returns the smallest value from a supplied set of numeric
// values. The syntax of the function is:
//
//	MIN(number1,[number2],...)
func (fn *formulaFuncs) MIN(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "MIN requires at least 1 argument")
	}
	return fn.min(false, argsList)
}

// MINA function returns the smallest value from a supplied set of numeric
// values, while counting text and the logical value FALSE as the value 0 and
// counting the logical value TRUE as the value 1. The syntax of the function
// is:
//
//	MINA(number1,[number2],...)
func (fn *formulaFuncs) MINA(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "MINA requires at least 1 argument")
	}
	return fn.min(true, argsList)
}

// MINIFS function returns the minimum value from a subset of values that are
// specified according to one or more criteria. The syntax of the function
// is:
//
//	MINIFS(min_range,criteria_range1,criteria1,[criteria_range2,criteria2],...)
func (fn *formulaFuncs) MINIFS(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "MINIFS requires at least 3 arguments")
	}
	if argsList.Len()%2 != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var args []formulaArg
	min, minRange := math.MaxFloat64, argsList.Front().Value.(formulaArg).Matrix
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		args = append(args, arg.Value.(formulaArg))
	}
	for _, ref := range formulaIfsMatch(args) {
		if num := minRange[ref.Row][ref.Col].ToNumber(); num.Type == ArgNumber && min > num.Number {
			min = num.Number
		}
	}
	if min == math.MaxFloat64 {
		min = 0
	}
	return newNumberFormulaArg(min)
}

// calcListMatrixMin is part of the implementation min.
func calcListMatrixMin(mina bool, min float64, arg formulaArg) float64 {
	for _, cell := range arg.ToList() {
		if cell.Type == ArgNumber && cell.Number < min {
			if mina && cell.Boolean || !cell.Boolean {
				min = cell.Number
			}
		}
	}
	return min
}

// min is an implementation of the formula functions MIN and MINA.
func (fn *formulaFuncs) min(mina bool, argsList *list.List) formulaArg {
	min := math.MaxFloat64
	for token := argsList.Front(); token != nil; token = token.Next() {
		arg := token.Value.(formulaArg)
		switch arg.Type {
		case ArgString:
			if !mina && (arg.Value() == "TRUE" || arg.Value() == "FALSE") {
				continue
			} else {
				num := arg.ToBool()
				if num.Type == ArgNumber && num.Number < min {
					min = num.Number
					continue
				}
			}
			num := arg.ToNumber()
			if num.Type != ArgError && num.Number < min {
				min = num.Number
			}
		case ArgNumber:
			if arg.Number < min {
				min = arg.Number
			}
		case ArgList, ArgMatrix:
			min = calcListMatrixMin(mina, min, arg)
		case ArgError:
			return arg
		}
	}
	if min == math.MaxFloat64 {
		min = 0
	}
	return newNumberFormulaArg(min)
}

// pearsonProduct is an implementation of the formula functions FORECAST,
// FORECAST.LINEAR, INTERCEPT, PEARSON, RSQ and SLOPE.
func (fn *formulaFuncs) pearsonProduct(name string, n int, argsList *list.List) formulaArg {
	if argsList.Len() != n {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires %d arguments", name, n))
	}
	var fx formulaArg
	array1 := argsList.Back().Value.(formulaArg).ToList()
	array2 := argsList.Front().Value.(formulaArg).ToList()
	if name == "PEARSON" || name == "RSQ" {
		array1, array2 = array2, array1
	}
	if n == 3 {
		if fx = argsList.Front().Value.(formulaArg).ToNumber(); fx.Type != ArgNumber {
			return fx
		}
		array2 = argsList.Front().Next().Value.(formulaArg).ToList()
	}
	if len(array1) != len(array2) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var sum, deltaX, deltaY, x, y, length float64
	for i := 0; i < len(array1); i++ {
		num1, num2 := array1[i], array2[i]
		if !(num1.Type == ArgNumber && num2.Type == ArgNumber) {
			continue
		}
		x += num1.Number
		y += num2.Number
		length++
	}
	x /= length
	y /= length
	for i := 0; i < len(array1); i++ {
		num1, num2 := array1[i], array2[i]
		if !(num1.Type == ArgNumber && num2.Type == ArgNumber) {
			continue
		}
		sum += (num1.Number - x) * (num2.Number - y)
		deltaX += (num1.Number - x) * (num1.Number - x)
		deltaY += (num2.Number - y) * (num2.Number - y)
	}
	if sum*deltaX*deltaY == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(map[string]float64{
		"FORECAST":        y + sum/deltaX*(fx.Number-x),
		"FORECAST.LINEAR": y + sum/deltaX*(fx.Number-x),
		"INTERCEPT":       y - sum/deltaX*x,
		"PEARSON":         sum / math.Sqrt(deltaX*deltaY),
		"RSQ":             math.Pow(sum/math.Sqrt(deltaX*deltaY), 2),
		"SLOPE":           sum / deltaX,
	}[name])
}

// PEARSON function calculates the Pearson Product-Moment Correlation
// Coefficient for two sets of values. The syntax of the function is:
//
//	PEARSON(array1,array2)
func (fn *formulaFuncs) PEARSON(argsList *list.List) formulaArg {
	return fn.pearsonProduct("PEARSON", 2, argsList)
}

// PERCENTILEdotEXC function returns the k'th percentile (i.e. the value below
// which k% of the data values fall) for a supplied range of values and a
// supplied k (between 0 & 1 exclusive).The syntax of the function is:
//
//	PERCENTILE.EXC(array,k)
func (fn *formulaFuncs) PERCENTILEdotEXC(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "PERCENTILE.EXC requires 2 arguments")
	}
	array := argsList.Front().Value.(formulaArg).ToList()
	k := argsList.Back().Value.(formulaArg).ToNumber()
	if k.Type != ArgNumber {
		return k
	}
	if k.Number <= 0 || k.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	var numbers []float64
	for _, arg := range array {
		if arg.Type == ArgError {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		if arg.Type == ArgNumber {
			numbers = append(numbers, arg.Number)
		}
	}
	cnt := len(numbers)
	sort.Float64s(numbers)
	idx := k.Number * (float64(cnt) + 1)
	base := math.Floor(idx)
	next := base - 1
	proportion := math.Nextafter(idx, idx) - base
	return newNumberFormulaArg(numbers[int(next)] + ((numbers[int(base)] - numbers[int(next)]) * proportion))
}

// PERCENTILEdotINC function returns the k'th percentile (i.e. the value below
// which k% of the data values fall) for a supplied range of values and a
// supplied k. The syntax of the function is:
//
//	PERCENTILE.INC(array,k)
func (fn *formulaFuncs) PERCENTILEdotINC(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "PERCENTILE.INC requires 2 arguments")
	}
	return fn.PERCENTILE(argsList)
}

// PERCENTILE function returns the k'th percentile (i.e. the value below which
// k% of the data values fall) for a supplied range of values and a supplied
// k. The syntax of the function is:
//
//	PERCENTILE(array,k)
func (fn *formulaFuncs) PERCENTILE(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "PERCENTILE requires 2 arguments")
	}
	array := argsList.Front().Value.(formulaArg).ToList()
	k := argsList.Back().Value.(formulaArg).ToNumber()
	if k.Type != ArgNumber {
		return k
	}
	if k.Number < 0 || k.Number > 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var numbers []float64
	for _, arg := range array {
		if arg.Type == ArgError {
			return arg
		}
		if arg.Type == ArgNumber {
			numbers = append(numbers, arg.Number)
		}
	}
	cnt := len(numbers)
	sort.Float64s(numbers)
	idx := k.Number * (float64(cnt) - 1)
	base := math.Floor(idx)
	if idx == base {
		return newNumberFormulaArg(numbers[int(idx)])
	}
	next := base + 1
	proportion := math.Nextafter(idx, idx) - base
	return newNumberFormulaArg(numbers[int(base)] + ((numbers[int(next)] - numbers[int(base)]) * proportion))
}

// percentrank is an implementation of the formula functions PERCENTRANK and
// PERCENTRANK.INC.
func (fn *formulaFuncs) percentrank(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 && argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 or 3 arguments", name))
	}
	array := argsList.Front().Value.(formulaArg).ToList()
	x := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	var numbers []float64
	for _, arg := range array {
		if arg.Type == ArgError {
			return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
		}
		if arg.Type == ArgNumber {
			numbers = append(numbers, arg.Number)
		}
	}
	cnt := len(numbers)
	sort.Float64s(numbers)
	if x.Number < numbers[0] || x.Number > numbers[cnt-1] {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	pos, significance := float64(inFloat64Slice(numbers, x.Number)), newNumberFormulaArg(3)
	if argsList.Len() == 3 {
		if significance = argsList.Back().Value.(formulaArg).ToNumber(); significance.Type != ArgNumber {
			return significance
		}
		if significance.Number < 1 {
			return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s arguments significance should be > 1", name))
		}
	}
	if pos == -1 {
		pos = 0
		cmp := numbers[0]
		for cmp < x.Number {
			pos++
			cmp = numbers[int(pos)]
		}
		pos--
		pos += (x.Number - numbers[int(pos)]) / (cmp - numbers[int(pos)])
	}
	pow := math.Pow(10, significance.Number)
	digit := pow * pos / (float64(cnt) - 1)
	if name == "PERCENTRANK.EXC" {
		digit = pow * (pos + 1) / (float64(cnt) + 1)
	}
	return newNumberFormulaArg(math.Floor(digit) / pow)
}

// PERCENTRANKdotEXC function calculates the relative position, between 0 and
// 1 (exclusive), of a specified value within a supplied array. The syntax of
// the function is:
//
//	PERCENTRANK.EXC(array,x,[significance])
func (fn *formulaFuncs) PERCENTRANKdotEXC(argsList *list.List) formulaArg {
	return fn.percentrank("PERCENTRANK.EXC", argsList)
}

// PERCENTRANKdotINC function calculates the relative position, between 0 and
// 1 (inclusive), of a specified value within a supplied array.The syntax of
// the function is:
//
//	PERCENTRANK.INC(array,x,[significance])
func (fn *formulaFuncs) PERCENTRANKdotINC(argsList *list.List) formulaArg {
	return fn.percentrank("PERCENTRANK.INC", argsList)
}

// PERCENTRANK function calculates the relative position of a specified value,
// within a set of values, as a percentage. The syntax of the function is:
//
//	PERCENTRANK(array,x,[significance])
func (fn *formulaFuncs) PERCENTRANK(argsList *list.List) formulaArg {
	return fn.percentrank("PERCENTRANK", argsList)
}

// PERMUT function calculates the number of permutations of a specified number
// of objects from a set of objects. The syntax of the function is:
//
//	PERMUT(number,number_chosen)
func (fn *formulaFuncs) PERMUT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "PERMUT requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	chosen := argsList.Back().Value.(formulaArg).ToNumber()
	if number.Type != ArgNumber {
		return number
	}
	if chosen.Type != ArgNumber {
		return chosen
	}
	if number.Number < chosen.Number {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(math.Round(fact(number.Number) / fact(number.Number-chosen.Number)))
}

// PERMUTATIONA function calculates the number of permutations, with
// repetitions, of a specified number of objects from a set. The syntax of
// the function is:
//
//	PERMUTATIONA(number,number_chosen)
func (fn *formulaFuncs) PERMUTATIONA(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "PERMUTATIONA requires 2 numeric arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	chosen := argsList.Back().Value.(formulaArg).ToNumber()
	if number.Type != ArgNumber {
		return number
	}
	if chosen.Type != ArgNumber {
		return chosen
	}
	num, numChosen := math.Floor(number.Number), math.Floor(chosen.Number)
	if num < 0 || numChosen < 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(math.Pow(num, numChosen))
}

// PHI function returns the value of the density function for a standard normal
// distribution for a supplied number. The syntax of the function is:
//
//	PHI(x)
func (fn *formulaFuncs) PHI(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "PHI requires 1 argument")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	return newNumberFormulaArg(0.39894228040143268 * math.Exp(-(x.Number*x.Number)/2))
}

// QUARTILE function returns a requested quartile of a supplied range of
// values. The syntax of the function is:
//
//	QUARTILE(array,quart)
func (fn *formulaFuncs) QUARTILE(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "QUARTILE requires 2 arguments")
	}
	quart := argsList.Back().Value.(formulaArg).ToNumber()
	if quart.Type != ArgNumber {
		return quart
	}
	if quart.Number < 0 || quart.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(newNumberFormulaArg(quart.Number / 4))
	return fn.PERCENTILE(args)
}

// QUARTILEdotEXC function returns a requested quartile of a supplied range of
// values, based on a percentile range of 0 to 1 exclusive. The syntax of the
// function is:
//
//	QUARTILE.EXC(array,quart)
func (fn *formulaFuncs) QUARTILEdotEXC(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "QUARTILE.EXC requires 2 arguments")
	}
	quart := argsList.Back().Value.(formulaArg).ToNumber()
	if quart.Type != ArgNumber {
		return quart
	}
	if quart.Number <= 0 || quart.Number >= 4 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(newNumberFormulaArg(quart.Number / 4))
	return fn.PERCENTILEdotEXC(args)
}

// QUARTILEdotINC function returns a requested quartile of a supplied range of
// values. The syntax of the function is:
//
//	QUARTILE.INC(array,quart)
func (fn *formulaFuncs) QUARTILEdotINC(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "QUARTILE.INC requires 2 arguments")
	}
	return fn.QUARTILE(argsList)
}

// rank is an implementation of the formula functions RANK and RANK.EQ.
func (fn *formulaFuncs) rank(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 2 arguments", name))
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at most 3 arguments", name))
	}
	num := argsList.Front().Value.(formulaArg).ToNumber()
	if num.Type != ArgNumber {
		return num
	}
	var arr []float64
	for _, arg := range argsList.Front().Next().Value.(formulaArg).ToList() {
		if arg.Type == ArgNumber {
			arr = append(arr, arg.Number)
		}
	}
	sort.Float64s(arr)
	order := newNumberFormulaArg(0)
	if argsList.Len() == 3 {
		if order = argsList.Back().Value.(formulaArg).ToNumber(); order.Type != ArgNumber {
			return order
		}
	}
	if order.Number == 0 {
		sort.Sort(sort.Reverse(sort.Float64Slice(arr)))
	}
	if idx := inFloat64Slice(arr, num.Number); idx != -1 {
		return newNumberFormulaArg(float64(idx + 1))
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// RANKdotEQ function returns the statistical rank of a given value, within a
// supplied array of values. If there are duplicate values in the list, these
// are given the same rank. The syntax of the function is:
//
//	RANK.EQ(number,ref,[order])
func (fn *formulaFuncs) RANKdotEQ(argsList *list.List) formulaArg {
	return fn.rank("RANK.EQ", argsList)
}

// RANK function returns the statistical rank of a given value, within a
// supplied array of values. If there are duplicate values in the list, these
// are given the same rank. The syntax of the function is:
//
//	RANK(number,ref,[order])
func (fn *formulaFuncs) RANK(argsList *list.List) formulaArg {
	return fn.rank("RANK", argsList)
}

// RSQ function calculates the square of the Pearson Product-Moment Correlation
// Coefficient for two supplied sets of values. The syntax of the function
// is:
//
//	RSQ(known_y's,known_x's)
func (fn *formulaFuncs) RSQ(argsList *list.List) formulaArg {
	return fn.pearsonProduct("RSQ", 2, argsList)
}

// skew is an implementation of the formula functions SKEW and SKEW.P.
func (fn *formulaFuncs) skew(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	mean := fn.AVERAGE(argsList)
	var stdDev formulaArg
	var count, summer float64
	if name == "SKEW" {
		stdDev = fn.STDEV(argsList)
	} else {
		stdDev = fn.STDEVP(argsList)
	}
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgNumber, ArgString:
			num := token.ToNumber()
			if num.Type == ArgError {
				return num
			}
			summer += math.Pow((num.Number-mean.Number)/stdDev.Number, 3)
			count++
		case ArgList, ArgMatrix:
			for _, cell := range token.ToList() {
				if cell.Type != ArgNumber {
					continue
				}
				summer += math.Pow((cell.Number-mean.Number)/stdDev.Number, 3)
				count++
			}
		}
	}
	if count > 2 {
		if name == "SKEW" {
			return newNumberFormulaArg(summer * (count / ((count - 1) * (count - 2))))
		}
		return newNumberFormulaArg(summer / count)
	}
	return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
}

// SKEW function calculates the skewness of the distribution of a supplied set
// of values. The syntax of the function is:
//
//	SKEW(number1,[number2],...)
func (fn *formulaFuncs) SKEW(argsList *list.List) formulaArg {
	return fn.skew("SKEW", argsList)
}

// SKEWdotP function calculates the skewness of the distribution of a supplied
// set of values. The syntax of the function is:
//
//	SKEW.P(number1,[number2],...)
func (fn *formulaFuncs) SKEWdotP(argsList *list.List) formulaArg {
	return fn.skew("SKEW.P", argsList)
}

// SLOPE returns the slope of the linear regression line through data points in
// known_y's and known_x's. The slope is the vertical distance divided by the
// horizontal distance between any two points on the line, which is the rate
// of change along the regression line. The syntax of the function is:
//
//	SLOPE(known_y's,known_x's)
func (fn *formulaFuncs) SLOPE(argsList *list.List) formulaArg {
	return fn.pearsonProduct("SLOPE", 2, argsList)
}

// SMALL function returns the k'th smallest value from an array of numeric
// values. The syntax of the function is:
//
//	SMALL(array,k)
func (fn *formulaFuncs) SMALL(argsList *list.List) formulaArg {
	return fn.kth("SMALL", argsList)
}

// STANDARDIZE function returns a normalized value of a distribution that is
// characterized by a supplied mean and standard deviation. The syntax of the
// function is:
//
//	STANDARDIZE(x,mean,standard_dev)
func (fn *formulaFuncs) STANDARDIZE(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "STANDARDIZE requires 3 arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	if x.Type != ArgNumber {
		return x
	}
	mean := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if mean.Type != ArgNumber {
		return mean
	}
	stdDev := argsList.Back().Value.(formulaArg).ToNumber()
	if stdDev.Type != ArgNumber {
		return stdDev
	}
	if stdDev.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg((x.Number - mean.Number) / stdDev.Number)
}

// stdevp is an implementation of the formula functions STDEVP, STDEV.P and
// STDEVPA.
func (fn *formulaFuncs) stdevp(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	fnName := "VARP"
	if name == "STDEVPA" {
		fnName = "VARPA"
	}
	varp := fn.vars(fnName, argsList)
	if varp.Type != ArgNumber {
		return varp
	}
	return newNumberFormulaArg(math.Sqrt(varp.Number))
}

// STDEVP function calculates the standard deviation of a supplied set of
// values. The syntax of the function is:
//
//	STDEVP(number1,[number2],...)
func (fn *formulaFuncs) STDEVP(argsList *list.List) formulaArg {
	return fn.stdevp("STDEVP", argsList)
}

// STDEVdotP function calculates the standard deviation of a supplied set of
// values.
//
//	STDEV.P( number1, [number2], ... )
func (fn *formulaFuncs) STDEVdotP(argsList *list.List) formulaArg {
	return fn.stdevp("STDEV.P", argsList)
}

// STDEVPA function calculates the standard deviation of a supplied set of
// values. The syntax of the function is:
//
//	STDEVPA(number1,[number2],...)
func (fn *formulaFuncs) STDEVPA(argsList *list.List) formulaArg {
	return fn.stdevp("STDEVPA", argsList)
}

// STEYX function calculates the standard error for the line of best fit,
// through a supplied set of x- and y- values. The syntax of the function is:
//
//	STEYX(known_y's,known_x's)
func (fn *formulaFuncs) STEYX(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "STEYX requires 2 arguments")
	}
	array1 := argsList.Back().Value.(formulaArg).ToList()
	array2 := argsList.Front().Value.(formulaArg).ToList()
	if len(array1) != len(array2) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	var count, sumX, sumY, squareX, squareY, sigmaXY float64
	for i := 0; i < len(array1); i++ {
		num1, num2 := array1[i], array2[i]
		if !(num1.Type == ArgNumber && num2.Type == ArgNumber) {
			continue
		}
		sumX += num1.Number
		sumY += num2.Number
		squareX += num1.Number * num1.Number
		squareY += num2.Number * num2.Number
		sigmaXY += num1.Number * num2.Number
		count++
	}
	if count < 3 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	dx, dy := sumX/count, sumY/count
	sigma1 := squareY - 2*dy*sumY + count*dy*dy
	sigma2 := sigmaXY - dy*sumX - sumY*dx + count*dy*dx
	sigma3 := squareX - 2*dx*sumX + count*dx*dx
	return newNumberFormulaArg(math.Sqrt((sigma1 - (sigma2*sigma2)/sigma3) / (count - 2)))
}

// getTDist is an implementation for the beta distribution probability density
// function.
func getTDist(T, fDF, nType float64) float64 {
	var res float64
	switch nType {
	case 1:
		res = 0.5 * getBetaDist(fDF/(fDF+T*T), fDF/2, 0.5)
	case 2:
		res = getBetaDist(fDF/(fDF+T*T), fDF/2, 0.5)
	case 3:
		res = math.Pow(1+(T*T/fDF), -(fDF+1)/2) / (math.Sqrt(fDF) * getBeta(0.5, fDF/2.0))
	case 4:
		X := fDF / (T*T + fDF)
		R := 0.5 * getBetaDist(X, 0.5*fDF, 0.5)
		res = 1 - R
		if T < 0 {
			res = R
		}
	}
	return res
}

// TdotDIST function calculates the one-tailed Student's T Distribution, which
// is a continuous probability distribution that is frequently used for
// testing hypotheses on small sample data sets. The syntax of the function
// is:
//
//	T.DIST(x,degrees_freedom,cumulative)
func (fn *formulaFuncs) TdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.DIST requires 3 arguments")
	}
	var x, degrees, cumulative formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if degrees = argsList.Front().Next().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if cumulative = argsList.Back().Value.(formulaArg).ToBool(); cumulative.Type != ArgNumber {
		return cumulative
	}
	if cumulative.Number == 1 && degrees.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if cumulative.Number == 0 {
		if degrees.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		if degrees.Number == 0 {
			return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
		}
		return newNumberFormulaArg(getTDist(x.Number, degrees.Number, 3))
	}
	return newNumberFormulaArg(getTDist(x.Number, degrees.Number, 4))
}

// TdotDISTdot2T function calculates the two-tailed Student's T Distribution,
// which is a continuous probability distribution that is frequently used for
// testing hypotheses on small sample data sets. The syntax of the function
// is:
//
//	T.DIST.2T(x,degrees_freedom)
func (fn *formulaFuncs) TdotDISTdot2T(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.DIST.2T requires 2 arguments")
	}
	var x, degrees formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if degrees = argsList.Back().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if x.Number < 0 || degrees.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(getTDist(x.Number, degrees.Number, 2))
}

// TdotDISTdotRT function calculates the right-tailed Student's T Distribution,
// which is a continuous probability distribution that is frequently used for
// testing hypotheses on small sample data sets. The syntax of the function
// is:
//
//	T.DIST.RT(x,degrees_freedom)
func (fn *formulaFuncs) TdotDISTdotRT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.DIST.RT requires 2 arguments")
	}
	var x, degrees formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if degrees = argsList.Back().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if degrees.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	v := getTDist(x.Number, degrees.Number, 1)
	if x.Number < 0 {
		v = 1 - v
	}
	return newNumberFormulaArg(v)
}

// TDIST function calculates the Student's T Distribution, which is a
// continuous probability distribution that is frequently used for testing
// hypotheses on small sample data sets. The syntax of the function is:
//
//	TDIST(x,degrees_freedom,tails)
func (fn *formulaFuncs) TDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TDIST requires 3 arguments")
	}
	var x, degrees, tails formulaArg
	if x = argsList.Front().Value.(formulaArg).ToNumber(); x.Type != ArgNumber {
		return x
	}
	if degrees = argsList.Front().Next().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if tails = argsList.Back().Value.(formulaArg).ToNumber(); tails.Type != ArgNumber {
		return tails
	}
	if x.Number < 0 || degrees.Number < 1 || (tails.Number != 1 && tails.Number != 2) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(getTDist(x.Number, degrees.Number, tails.Number))
}

// TdotINV function calculates the left-tailed inverse of the Student's T
// Distribution, which is a continuous probability distribution that is
// frequently used for testing hypotheses on small sample data sets. The
// syntax of the function is:
//
//	T.INV(probability,degrees_freedom)
func (fn *formulaFuncs) TdotINV(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.INV requires 2 arguments")
	}
	var probability, degrees formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if degrees = argsList.Back().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if probability.Number <= 0 || probability.Number >= 1 || degrees.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if probability.Number < 0.5 {
		return newNumberFormulaArg(-calcIterateInverse(calcInverseIterator{
			name: "T.INV",
			fp:   1 - probability.Number,
			fDF:  degrees.Number,
			nT:   4,
		}, degrees.Number/2, degrees.Number))
	}
	return newNumberFormulaArg(calcIterateInverse(calcInverseIterator{
		name: "T.INV",
		fp:   probability.Number,
		fDF:  degrees.Number,
		nT:   4,
	}, degrees.Number/2, degrees.Number))
}

// TdotINVdot2T function calculates the inverse of the two-tailed Student's T
// Distribution, which is a continuous probability distribution that is
// frequently used for testing hypotheses on small sample data sets. The
// syntax of the function is:
//
//	T.INV.2T(probability,degrees_freedom)
func (fn *formulaFuncs) TdotINVdot2T(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.INV.2T requires 2 arguments")
	}
	var probability, degrees formulaArg
	if probability = argsList.Front().Value.(formulaArg).ToNumber(); probability.Type != ArgNumber {
		return probability
	}
	if degrees = argsList.Back().Value.(formulaArg).ToNumber(); degrees.Type != ArgNumber {
		return degrees
	}
	if probability.Number <= 0 || probability.Number > 1 || degrees.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(calcIterateInverse(calcInverseIterator{
		name: "T.INV.2T",
		fp:   probability.Number,
		fDF:  degrees.Number,
		nT:   2,
	}, degrees.Number/2, degrees.Number))
}

// TINV function calculates the inverse of the two-tailed Student's T
// Distribution, which is a continuous probability distribution that is
// frequently used for testing hypotheses on small sample data sets. The
// syntax of the function is:
//
//	TINV(probability,degrees_freedom)
func (fn *formulaFuncs) TINV(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "TINV requires 2 arguments")
	}
	return fn.TdotINVdot2T(argsList)
}

// TREND function calculates the linear trend line through a given set of
// y-values and (optionally), a given set of x-values. The function then
// extends the linear trendline to calculate additional y-values for a further
// supplied set of new x-values. The syntax of the function is:
//
//	TREND(known_y's,[known_x's],[new_x's],[const])
func (fn *formulaFuncs) TREND(argsList *list.List) formulaArg {
	return fn.trendGrowth("TREND", argsList)
}

// tTest calculates the probability associated with the Student's T Test.
func tTest(bTemplin bool, mtx1, mtx2 [][]formulaArg, c1, c2, r1, r2 int) (float64, float64, bool) {
	var cnt1, cnt2, sum1, sumSqr1, sum2, sumSqr2 float64
	var fVal formulaArg
	for i := 0; i < c1; i++ {
		for j := 0; j < r1; j++ {
			if fVal = mtx1[i][j]; fVal.Type == ArgNumber {
				sum1 += fVal.Number
				sumSqr1 += fVal.Number * fVal.Number
				cnt1++
			}
		}
	}
	for i := 0; i < c2; i++ {
		for j := 0; j < r2; j++ {
			if fVal = mtx2[i][j]; fVal.Type == ArgNumber {
				sum2 += fVal.Number
				sumSqr2 += fVal.Number * fVal.Number
				cnt2++
			}
		}
	}
	if cnt1 < 2.0 || cnt2 < 2.0 {
		return 0, 0, false
	}
	if bTemplin {
		fS1 := (sumSqr1 - sum1*sum1/cnt1) / (cnt1 - 1) / cnt1
		fS2 := (sumSqr2 - sum2*sum2/cnt2) / (cnt2 - 1) / cnt2
		if fS1+fS2 == 0 {
			return 0, 0, false
		}
		c := fS1 / (fS1 + fS2)
		return math.Abs(sum1/cnt1-sum2/cnt2) / math.Sqrt(fS1+fS2), 1 / (c*c/(cnt1-1) + (1-c)*(1-c)/(cnt2-1)), true
	}
	fS1 := (sumSqr1 - sum1*sum1/cnt1) / (cnt1 - 1)
	fS2 := (sumSqr2 - sum2*sum2/cnt2) / (cnt2 - 1)
	return math.Abs(sum1/cnt1-sum2/cnt2) / math.Sqrt((cnt1-1)*fS1+(cnt2-1)*fS2) * math.Sqrt(cnt1*cnt2*(cnt1+cnt2-2)/(cnt1+cnt2)), cnt1 + cnt2 - 2, true
}

// tTest is an implementation of the formula function TTEST.
func (fn *formulaFuncs) tTest(mtx1, mtx2 [][]formulaArg, fTails, fTyp float64) formulaArg {
	var fT, fF float64
	c1, c2, r1, r2, ok := len(mtx1), len(mtx2), len(mtx1[0]), len(mtx2[0]), true
	if fTyp == 1 {
		if c1 != c2 || r1 != r2 {
			return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
		}
		var cnt, sum1, sum2, sumSqrD float64
		var fVal1, fVal2 formulaArg
		for i := 0; i < c1; i++ {
			for j := 0; j < r1; j++ {
				fVal1, fVal2 = mtx1[i][j], mtx2[i][j]
				if fVal1.Type != ArgNumber || fVal2.Type != ArgNumber {
					continue
				}
				sum1 += fVal1.Number
				sum2 += fVal2.Number
				sumSqrD += (fVal1.Number - fVal2.Number) * (fVal1.Number - fVal2.Number)
				cnt++
			}
		}
		if cnt < 1 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		sumD := sum1 - sum2
		divider := cnt*sumSqrD - sumD*sumD
		if divider == 0 {
			return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
		}
		fT = math.Abs(sumD) * math.Sqrt((cnt-1)/divider)
		fF = cnt - 1
	} else if fTyp == 2 {
		fT, fF, ok = tTest(false, mtx1, mtx2, c1, c2, r1, r2)
	} else {
		fT, fF, ok = tTest(true, mtx1, mtx2, c1, c2, r1, r2)
	}
	if !ok {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(getTDist(fT, fF, fTails))
}

// TTEST function calculates the probability associated with the Student's T
// Test, which is commonly used for identifying whether two data sets are
// likely to have come from the same two underlying populations with the same
// mean. The syntax of the function is:
//
//	TTEST(array1,array2,tails,type)
func (fn *formulaFuncs) TTEST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "TTEST requires 4 arguments")
	}
	var array1, array2, tails, typeArg formulaArg
	array1 = argsList.Front().Value.(formulaArg)
	array2 = argsList.Front().Next().Value.(formulaArg)
	if tails = argsList.Front().Next().Next().Value.(formulaArg); tails.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if typeArg = argsList.Back().Value.(formulaArg); typeArg.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if len(array1.Matrix) == 0 || len(array2.Matrix) == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if tails.Number != 1 && tails.Number != 2 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if typeArg.Number != 1 && typeArg.Number != 2 && typeArg.Number != 3 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return fn.tTest(array1.Matrix, array2.Matrix, tails.Number, typeArg.Number)
}

// TdotTEST function calculates the probability associated with the Student's T
// Test, which is commonly used for identifying whether two data sets are
// likely to have come from the same two underlying populations with the same
// mean. The syntax of the function is:
//
//	T.TEST(array1,array2,tails,type)
func (fn *formulaFuncs) TdotTEST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "T.TEST requires 4 arguments")
	}
	return fn.TTEST(argsList)
}

// TRIMMEAN function calculates the trimmed mean (or truncated mean) of a
// supplied set of values. The syntax of the function is:
//
//	TRIMMEAN(array,percent)
func (fn *formulaFuncs) TRIMMEAN(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "TRIMMEAN requires 2 arguments")
	}
	percent := argsList.Back().Value.(formulaArg).ToNumber()
	if percent.Type != ArgNumber {
		return percent
	}
	if percent.Number < 0 || percent.Number >= 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	var arr []float64
	arrArg := argsList.Front().Value.(formulaArg).ToList()
	for _, cell := range arrArg {
		if cell.Type != ArgNumber {
			continue
		}
		arr = append(arr, cell.Number)
	}
	discard := math.Floor(float64(len(arr)) * percent.Number / 2)
	sort.Float64s(arr)
	for i := 0; i < int(discard); i++ {
		if len(arr) > 0 {
			arr = arr[1:]
		}
		if len(arr) > 0 {
			arr = arr[:len(arr)-1]
		}
	}

	args := list.New().Init()
	for _, ele := range arr {
		args.PushBack(newNumberFormulaArg(ele))
	}
	return fn.AVERAGE(args)
}

// vars is an implementation of the formula functions VAR, VARA, VARP, VAR.P
// VAR.S and VARPA.
func (fn *formulaFuncs) vars(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	summerA, summerB, count := 0.0, 0.0, 0.0
	minimum := 0.0
	if name == "VAR" || name == "VAR.S" || name == "VARA" {
		minimum = 1.0
	}
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		for _, token := range arg.Value.(formulaArg).ToList() {
			if token.Value() == "" {
				continue
			}
			num := token.ToNumber()
			if token.Value() != "TRUE" && num.Type == ArgNumber {
				summerA += num.Number * num.Number
				summerB += num.Number
				count++
				continue
			}
			num = token.ToBool()
			if num.Type == ArgNumber {
				summerA += num.Number * num.Number
				summerB += num.Number
				count++
				continue
			}
			if name == "VARA" || name == "VARPA" {
				count++
			}
		}
	}
	if count > minimum {
		summerA *= count
		summerB *= summerB
		return newNumberFormulaArg((summerA - summerB) / (count * (count - minimum)))
	}
	return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
}

// VAR function returns the sample variance of a supplied set of values. The
// syntax of the function is:
//
//	VAR(number1,[number2],...)
func (fn *formulaFuncs) VAR(argsList *list.List) formulaArg {
	return fn.vars("VAR", argsList)
}

// VARA function calculates the sample variance of a supplied set of values.
// The syntax of the function is:
//
//	VARA(number1,[number2],...)
func (fn *formulaFuncs) VARA(argsList *list.List) formulaArg {
	return fn.vars("VARA", argsList)
}

// VARP function returns the Variance of a given set of values. The syntax of
// the function is:
//
//	VARP(number1,[number2],...)
func (fn *formulaFuncs) VARP(argsList *list.List) formulaArg {
	return fn.vars("VARP", argsList)
}

// VARdotP function returns the Variance of a given set of values. The syntax
// of the function is:
//
//	VAR.P(number1,[number2],...)
func (fn *formulaFuncs) VARdotP(argsList *list.List) formulaArg {
	return fn.vars("VAR.P", argsList)
}

// VARdotS function calculates the sample variance of a supplied set of
// values. The syntax of the function is:
//
//	VAR.S(number1,[number2],...)
func (fn *formulaFuncs) VARdotS(argsList *list.List) formulaArg {
	return fn.vars("VAR.S", argsList)
}

// VARPA function returns the Variance of a given set of values. The syntax of
// the function is:
//
//	VARPA(number1,[number2],...)
func (fn *formulaFuncs) VARPA(argsList *list.List) formulaArg {
	return fn.vars("VARPA", argsList)
}

// WEIBULL function calculates the Weibull Probability Density Function or the
// Weibull Cumulative Distribution Function for a supplied set of parameters.
// The syntax of the function is:
//
//	WEIBULL(x,alpha,beta,cumulative)
func (fn *formulaFuncs) WEIBULL(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEIBULL requires 4 arguments")
	}
	x := argsList.Front().Value.(formulaArg).ToNumber()
	alpha := argsList.Front().Next().Value.(formulaArg).ToNumber()
	beta := argsList.Back().Prev().Value.(formulaArg).ToNumber()
	if alpha.Type == ArgNumber && beta.Type == ArgNumber && x.Type == ArgNumber {
		if alpha.Number < 0 || alpha.Number <= 0 || beta.Number <= 0 {
			return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
		}
		cumulative := argsList.Back().Value.(formulaArg).ToBool()
		if cumulative.Boolean && cumulative.Number == 1 {
			return newNumberFormulaArg(1 - math.Exp(0-math.Pow(x.Number/beta.Number, alpha.Number)))
		}
		return newNumberFormulaArg((alpha.Number / math.Pow(beta.Number, alpha.Number)) *
			math.Pow(x.Number, alpha.Number-1) * math.Exp(0-math.Pow(x.Number/beta.Number, alpha.Number)))
	}
	return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
}

// WEIBULLdotDIST function calculates the Weibull Probability Density Function
// or the Weibull Cumulative Distribution Function for a supplied set of
// parameters. The syntax of the function is:
//
//	WEIBULL.DIST(x,alpha,beta,cumulative)
func (fn *formulaFuncs) WEIBULLdotDIST(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEIBULL.DIST requires 4 arguments")
	}
	return fn.WEIBULL(argsList)
}

// ZdotTEST function calculates the one-tailed probability value of the
// Z-Test. The syntax of the function is:
//
//	Z.TEST(array,x,[sigma])
func (fn *formulaFuncs) ZdotTEST(argsList *list.List) formulaArg {
	argsLen := argsList.Len()
	if argsLen < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "Z.TEST requires at least 2 arguments")
	}
	if argsLen > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "Z.TEST accepts at most 3 arguments")
	}
	return fn.ZTEST(argsList)
}

// ZTEST function calculates the one-tailed probability value of the Z-Test.
// The syntax of the function is:
//
//	ZTEST(array,x,[sigma])
func (fn *formulaFuncs) ZTEST(argsList *list.List) formulaArg {
	argsLen := argsList.Len()
	if argsLen < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ZTEST requires at least 2 arguments")
	}
	if argsLen > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "ZTEST accepts at most 3 arguments")
	}
	arrArg, arrArgs := argsList.Front().Value.(formulaArg), list.New()
	arrArgs.PushBack(arrArg)
	arr := fn.AVERAGE(arrArgs)
	if arr.Type == ArgError {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	x := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if x.Type == ArgError {
		return x
	}
	sigma := argsList.Back().Value.(formulaArg).ToNumber()
	if sigma.Type == ArgError {
		return sigma
	}
	if argsLen != 3 {
		sigma = fn.STDEV(arrArgs).ToNumber()
	}
	normsdistArg := list.New()
	div := sigma.Number / math.Sqrt(float64(len(arrArg.ToList())))
	if div == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	normsdistArg.PushBack(newNumberFormulaArg((arr.Number - x.Number) / div))
	return newNumberFormulaArg(1 - fn.NORMSDIST(normsdistArg).Number)
}

// Information Functions

// ERRORdotTYPE function receives an error value and returns an integer, that
// tells you the type of the supplied error. The syntax of the function is:
//
//	ERROR.TYPE(error_val)
func (fn *formulaFuncs) ERRORdotTYPE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ERROR.TYPE requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	if token.Type == ArgError {
		for i, errType := range []string{
			formulaErrorNULL, formulaErrorDIV, formulaErrorVALUE, formulaErrorREF,
			formulaErrorNAME, formulaErrorNUM, formulaErrorNA,
		} {
			if errType == token.String {
				return newNumberFormulaArg(float64(i) + 1)
			}
		}
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// ISBLANK function tests if a specified cell is blank (empty) and if so,
// returns TRUE; Otherwise the function returns FALSE. The syntax of the
// function is:
//
//	ISBLANK(value)
func (fn *formulaFuncs) ISBLANK(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISBLANK requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgUnknown, ArgEmpty:
		return newBoolFormulaArg(true)
	default:
		return newBoolFormulaArg(false)
	}
}

// ISERR function tests if an initial supplied expression (or value) returns
// any Excel Error, except the #N/A error. If so, the function returns the
// logical value TRUE; If the supplied value is not an error or is the #N/A
// error, the ISERR function returns FALSE. The syntax of the function is:
//
//	ISERR(value)
func (fn *formulaFuncs) ISERR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISERR requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	result := false
	if token.Type == ArgError {
		for _, errType := range []string{
			formulaErrorDIV, formulaErrorNAME, formulaErrorNUM,
			formulaErrorVALUE, formulaErrorREF, formulaErrorNULL,
			formulaErrorSPILL, formulaErrorCALC, formulaErrorGETTINGDATA,
		} {
			if errType == token.String {
				result = true
			}
		}
	}
	return newBoolFormulaArg(result)
}

// ISERROR function tests if an initial supplied expression (or value) returns
// an Excel Error, and if so, returns the logical value TRUE; Otherwise the
// function returns FALSE. The syntax of the function is:
//
//	ISERROR(value)
func (fn *formulaFuncs) ISERROR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISERROR requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	result := false
	if token.Type == ArgError {
		for _, errType := range []string{
			formulaErrorDIV, formulaErrorNAME, formulaErrorNA, formulaErrorNUM,
			formulaErrorVALUE, formulaErrorREF, formulaErrorNULL, formulaErrorSPILL,
			formulaErrorCALC, formulaErrorGETTINGDATA,
		} {
			if errType == token.String {
				result = true
			}
		}
	}
	return newBoolFormulaArg(result)
}

// ISEVEN function tests if a supplied number (or numeric expression)
// evaluates to an even number, and if so, returns TRUE; Otherwise, the
// function returns FALSE. The syntax of the function is:
//
//	ISEVEN(value)
func (fn *formulaFuncs) ISEVEN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISEVEN requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgEmpty:
		return newBoolFormulaArg(true)
	case ArgNumber, ArgString:
		num := token.ToNumber()
		if num.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		if num.Number == 1 {
			return newBoolFormulaArg(false)
		}
		return newBoolFormulaArg(num.Number == num.Number/2*2)
	default:
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
}

// ISFORMULA function tests if a specified cell contains a formula, and if so,
// returns TRUE; Otherwise, the function returns FALSE. The syntax of the
// function is:
//
//	ISFORMULA(reference)
func (fn *formulaFuncs) ISFORMULA(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISFORMULA requires 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg)
	if arg.cellRefs != nil && arg.cellRefs.Len() == 1 {
		ref := arg.cellRefs.Front().Value.(cellRef)
		cell, _ := CoordinatesToCellName(ref.Col, ref.Row)
		if formula, _ := fn.f.GetCellFormula(ref.Sheet, cell); len(formula) > 0 {
			return newBoolFormulaArg(true)
		}
	}
	return newBoolFormulaArg(false)
}

// ISLOGICAL function tests if a supplied value (or expression) returns a
// logical value (i.e. evaluates to True or False). If so, the function
// returns TRUE; Otherwise, it returns FALSE. The syntax of the function is:
//
//	ISLOGICAL(value)
func (fn *formulaFuncs) ISLOGICAL(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISLOGICAL requires 1 argument")
	}
	val := argsList.Front().Value.(formulaArg).Value()
	if strings.EqualFold("TRUE", val) || strings.EqualFold("FALSE", val) {
		return newBoolFormulaArg(true)
	}
	return newBoolFormulaArg(false)
}

// ISNA function tests if an initial supplied expression (or value) returns
// the Excel #N/A Error, and if so, returns TRUE; Otherwise the function
// returns FALSE. The syntax of the function is:
//
//	ISNA(value)
func (fn *formulaFuncs) ISNA(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISNA requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	result := "FALSE"
	if token.Type == ArgError && token.String == formulaErrorNA {
		result = "TRUE"
	}
	return newStringFormulaArg(result)
}

// ISNONTEXT function tests if a supplied value is text. If not, the
// function returns TRUE; If the supplied value is text, the function returns
// FALSE. The syntax of the function is:
//
//	ISNONTEXT(value)
func (fn *formulaFuncs) ISNONTEXT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISNONTEXT requires 1 argument")
	}
	if argsList.Front().Value.(formulaArg).Type == ArgString {
		return newBoolFormulaArg(false)
	}
	return newBoolFormulaArg(true)
}

// ISNUMBER function tests if a supplied value is a number. If so,
// the function returns TRUE; Otherwise it returns FALSE. The syntax of the
// function is:
//
//	ISNUMBER(value)
func (fn *formulaFuncs) ISNUMBER(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISNUMBER requires 1 argument")
	}
	if argsList.Front().Value.(formulaArg).Type == ArgNumber {
		return newBoolFormulaArg(true)
	}
	return newBoolFormulaArg(false)
}

// ISODD function tests if a supplied number (or numeric expression) evaluates
// to an odd number, and if so, returns TRUE; Otherwise, the function returns
// FALSE. The syntax of the function is:
//
//	ISODD(value)
func (fn *formulaFuncs) ISODD(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISODD requires 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if int(arg.Number) != int(arg.Number)/2*2 {
		return newBoolFormulaArg(true)
	}
	return newBoolFormulaArg(false)
}

// ISREF function tests if a supplied value is a reference. If so, the
// function returns TRUE; Otherwise it returns FALSE. The syntax of the
// function is:
//
//	ISREF(value)
func (fn *formulaFuncs) ISREF(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISREF requires 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg)
	if arg.cellRanges != nil && arg.cellRanges.Len() > 0 || arg.cellRefs != nil && arg.cellRefs.Len() > 0 {
		return newBoolFormulaArg(true)
	}
	return newBoolFormulaArg(false)
}

// ISTEXT function tests if a supplied value is text, and if so, returns TRUE;
// Otherwise, the function returns FALSE. The syntax of the function is:
//
//	ISTEXT(value)
func (fn *formulaFuncs) ISTEXT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISTEXT requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	if token.ToNumber().Type != ArgError {
		return newBoolFormulaArg(false)
	}
	return newBoolFormulaArg(token.Type == ArgString)
}

// N function converts data into a numeric value. The syntax of the function
// is:
//
//	N(value)
func (fn *formulaFuncs) N(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "N requires 1 argument")
	}
	token, num := argsList.Front().Value.(formulaArg), 0.0
	if token.Type == ArgError {
		return token
	}
	if arg := token.ToNumber(); arg.Type == ArgNumber {
		num = arg.Number
	}
	if token.Value() == "TRUE" {
		num = 1
	}
	return newNumberFormulaArg(num)
}

// NA function returns the Excel #N/A error. This error message has the
// meaning 'value not available' and is produced when an Excel Formula is
// unable to find a value that it needs. The syntax of the function is:
//
//	NA()
func (fn *formulaFuncs) NA(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "NA accepts no arguments")
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// SHEET function returns the Sheet number for a specified reference. The
// syntax of the function is:
//
//	SHEET([value])
func (fn *formulaFuncs) SHEET(argsList *list.List) formulaArg {
	if argsList.Len() > 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SHEET accepts at most 1 argument")
	}
	if argsList.Len() == 0 {
		idx, _ := fn.f.GetSheetIndex(fn.sheet)
		return newNumberFormulaArg(float64(idx + 1))
	}
	arg := argsList.Front().Value.(formulaArg)
	if sheetIdx, _ := fn.f.GetSheetIndex(arg.Value()); sheetIdx != -1 {
		return newNumberFormulaArg(float64(sheetIdx + 1))
	}
	if arg.cellRanges != nil && arg.cellRanges.Len() > 0 {
		if sheetIdx, _ := fn.f.GetSheetIndex(arg.cellRanges.Front().Value.(cellRange).From.Sheet); sheetIdx != -1 {
			return newNumberFormulaArg(float64(sheetIdx + 1))
		}
	}
	if arg.cellRefs != nil && arg.cellRefs.Len() > 0 {
		if sheetIdx, _ := fn.f.GetSheetIndex(arg.cellRefs.Front().Value.(cellRef).Sheet); sheetIdx != -1 {
			return newNumberFormulaArg(float64(sheetIdx + 1))
		}
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// SHEETS function returns the number of sheets in a supplied reference. The
// result includes sheets that are Visible, Hidden or Very Hidden. The syntax
// of the function is:
//
//	SHEETS([reference])
func (fn *formulaFuncs) SHEETS(argsList *list.List) formulaArg {
	if argsList.Len() > 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SHEETS accepts at most 1 argument")
	}
	if argsList.Len() == 0 {
		return newNumberFormulaArg(float64(len(fn.f.GetSheetList())))
	}
	arg := argsList.Front().Value.(formulaArg)
	sheetMap := map[string]struct{}{}
	if arg.cellRanges != nil && arg.cellRanges.Len() > 0 {
		for rng := arg.cellRanges.Front(); rng != nil; rng = rng.Next() {
			sheetMap[rng.Value.(cellRange).From.Sheet] = struct{}{}
		}
	}
	if arg.cellRefs != nil && arg.cellRefs.Len() > 0 {
		for ref := arg.cellRefs.Front(); ref != nil; ref = ref.Next() {
			sheetMap[ref.Value.(cellRef).Sheet] = struct{}{}
		}
	}
	if len(sheetMap) > 0 {
		return newNumberFormulaArg(float64(len(sheetMap)))
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// TYPE function returns an integer that represents the value's data type. The
// syntax of the function is:
//
//	TYPE(value)
func (fn *formulaFuncs) TYPE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TYPE requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgError:
		return newNumberFormulaArg(16)
	case ArgMatrix:
		return newNumberFormulaArg(64)
	case ArgNumber, ArgEmpty:
		if token.Boolean {
			return newNumberFormulaArg(4)
		}
		return newNumberFormulaArg(1)
	default:
		return newNumberFormulaArg(2)
	}
}

// T function tests if a supplied value is text and if so, returns the
// supplied text; Otherwise, the function returns an empty text string. The
// syntax of the function is:
//
//	T(value)
func (fn *formulaFuncs) T(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "T requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	if token.Type == ArgError {
		return token
	}
	if token.Type == ArgNumber {
		return newStringFormulaArg("")
	}
	return newStringFormulaArg(token.Value())
}

// Logical Functions

// AND function tests a number of supplied conditions and returns TRUE or
// FALSE. The syntax of the function is:
//
//	AND(logical_test1,[logical_test2],...)
func (fn *formulaFuncs) AND(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "AND requires at least 1 argument")
	}
	if argsList.Len() > 30 {
		return newErrorFormulaArg(formulaErrorVALUE, "AND accepts at most 30 arguments")
	}
	and := true
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgUnknown:
			continue
		case ArgString:
			if token.String == "TRUE" {
				continue
			}
			if token.String == "FALSE" {
				return newStringFormulaArg(token.String)
			}
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		case ArgNumber:
			and = and && token.Number != 0
		case ArgMatrix:
			// TODO
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	return newBoolFormulaArg(and)
}

// FALSE function returns the logical value FALSE. The syntax of the
// function is:
//
//	FALSE()
func (fn *formulaFuncs) FALSE(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "FALSE takes no arguments")
	}
	return newBoolFormulaArg(false)
}

// IFERROR function receives two values (or expressions) and tests if the
// first of these evaluates to an error. The syntax of the function is:
//
//	IFERROR(value,value_if_error)
func (fn *formulaFuncs) IFERROR(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IFERROR requires 2 arguments")
	}
	value := argsList.Front().Value.(formulaArg)
	if value.Type != ArgError {
		if value.Type == ArgEmpty {
			return newNumberFormulaArg(0)
		}
		return value
	}
	return argsList.Back().Value.(formulaArg)
}

// IFNA function tests if an initial supplied value (or expression) evaluates
// to the Excel #N/A error. If so, the function returns a second supplied
// value; Otherwise the function returns the first supplied value. The syntax
// of the function is:
//
//	IFNA(value,value_if_na)
func (fn *formulaFuncs) IFNA(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IFNA requires 2 arguments")
	}
	arg := argsList.Front().Value.(formulaArg)
	if arg.Type == ArgError && arg.String == formulaErrorNA {
		return argsList.Back().Value.(formulaArg)
	}
	return arg
}

// IFS function tests a number of supplied conditions and returns the result
// corresponding to the first condition that evaluates to TRUE. If none of
// the supplied conditions evaluate to TRUE, the function returns the #N/A
// error.
//
//	IFS(logical_test1,value_if_true1,[logical_test2,value_if_true2],...)
func (fn *formulaFuncs) IFS(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IFS requires at least 2 arguments")
	}
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		if arg.Value.(formulaArg).ToBool().Number == 1 {
			return arg.Next().Value.(formulaArg)
		}
		arg = arg.Next()
	}
	return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
}

// NOT function returns the opposite to a supplied logical value. The syntax
// of the function is:
//
//	NOT(logical)
func (fn *formulaFuncs) NOT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "NOT requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg)
	switch token.Type {
	case ArgString, ArgList:
		if strings.ToUpper(token.String) == "TRUE" {
			return newBoolFormulaArg(false)
		}
		if strings.ToUpper(token.String) == "FALSE" {
			return newBoolFormulaArg(true)
		}
	case ArgNumber:
		return newBoolFormulaArg(!(token.Number != 0))
	case ArgError:
		return token
	}
	return newErrorFormulaArg(formulaErrorVALUE, "NOT expects 1 boolean or numeric argument")
}

// OR function tests a number of supplied conditions and returns either TRUE
// or FALSE. The syntax of the function is:
//
//	OR(logical_test1,[logical_test2],...)
func (fn *formulaFuncs) OR(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "OR requires at least 1 argument")
	}
	if argsList.Len() > 30 {
		return newErrorFormulaArg(formulaErrorVALUE, "OR accepts at most 30 arguments")
	}
	var or bool
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgUnknown:
			continue
		case ArgString:
			if token.String == "FALSE" {
				continue
			}
			if token.String == "TRUE" {
				or = true
				continue
			}
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		case ArgNumber:
			if or = token.Number != 0; or {
				return newStringFormulaArg(strings.ToUpper(strconv.FormatBool(or)))
			}
		case ArgMatrix:
			// TODO
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	return newStringFormulaArg(strings.ToUpper(strconv.FormatBool(or)))
}

// SWITCH function compares a number of supplied values to a supplied test
// expression and returns a result corresponding to the first value that
// matches the test expression. A default value can be supplied, to be
// returned if none of the supplied values match the test expression. The
// syntax of the function is:
//
//	SWITCH(expression,value1,result1,[value2,result2],[value3,result3],...,[default])
func (fn *formulaFuncs) SWITCH(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "SWITCH requires at least 3 arguments")
	}
	target := argsList.Front().Value.(formulaArg)
	argCount := argsList.Len() - 1
	switchCount := int(math.Floor(float64(argCount) / 2))
	hasDefaultClause := argCount%2 != 0
	result := newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	if hasDefaultClause {
		result = argsList.Back().Value.(formulaArg)
	}
	if switchCount > 0 {
		arg := argsList.Front()
		for i := 0; i < switchCount; i++ {
			arg = arg.Next()
			if target.Value() == arg.Value.(formulaArg).Value() {
				result = arg.Next().Value.(formulaArg)
				break
			}
			arg = arg.Next()
		}
	}
	return result
}

// TRUE function returns the logical value TRUE. The syntax of the function
// is:
//
//	TRUE()
func (fn *formulaFuncs) TRUE(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "TRUE takes no arguments")
	}
	return newBoolFormulaArg(true)
}

// calcXor checking if numeric cell exists and count it by given arguments
// sequence for the formula function XOR.
func calcXor(argsList *list.List) formulaArg {
	count, ok := 0, false
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		token := arg.Value.(formulaArg)
		switch token.Type {
		case ArgError:
			return token
		case ArgNumber:
			ok = true
			if token.Number != 0 {
				count++
			}
		case ArgMatrix:
			for _, value := range token.ToList() {
				if num := value.ToNumber(); num.Type == ArgNumber {
					ok = true
					if num.Number != 0 {
						count++
					}
				}
			}
		}
	}
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newBoolFormulaArg(count%2 != 0)
}

// XOR function returns the Exclusive Or logical operation for one or more
// supplied conditions. I.e. the Xor function returns TRUE if an odd number
// of the supplied conditions evaluate to TRUE, and FALSE otherwise. The
// syntax of the function is:
//
//	XOR(logical_test1,[logical_test2],...)
func (fn *formulaFuncs) XOR(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "XOR requires at least 1 argument")
	}
	return calcXor(argsList)
}

// Date and Time Functions

// DATE returns a date, from a user-supplied year, month and day. The syntax
// of the function is:
//
//	DATE(year,month,day)
func (fn *formulaFuncs) DATE(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "DATE requires 3 number arguments")
	}
	year := argsList.Front().Value.(formulaArg).ToNumber()
	month := argsList.Front().Next().Value.(formulaArg).ToNumber()
	day := argsList.Back().Value.(formulaArg).ToNumber()
	if year.Type != ArgNumber || month.Type != ArgNumber || day.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, "DATE requires 3 number arguments")
	}
	d := makeDate(int(year.Number), time.Month(month.Number), int(day.Number))
	return newStringFormulaArg(timeFromExcelTime(daysBetween(excelMinTime1900.Unix(), d)+1, false).String())
}

// calcDateDif is an implementation of the formula function DATEDIF,
// calculation difference between two dates.
func calcDateDif(unit string, diff float64, seq []int, startArg, endArg formulaArg) float64 {
	ey, sy, em, sm, ed, sd := seq[0], seq[1], seq[2], seq[3], seq[4], seq[5]
	switch unit {
	case "d":
		diff = endArg.Number - startArg.Number
	case "md":
		smMD := em
		if ed < sd {
			smMD--
		}
		diff = endArg.Number - daysBetween(excelMinTime1900.Unix(), makeDate(ey, time.Month(smMD), sd)) - 1
	case "ym":
		diff = float64(em - sm)
		if ed < sd {
			diff--
		}
		if diff < 0 {
			diff += 12
		}
	case "yd":
		syYD := sy
		if em < sm || (em == sm && ed < sd) {
			syYD++
		}
		s := daysBetween(excelMinTime1900.Unix(), makeDate(syYD, time.Month(em), ed))
		e := daysBetween(excelMinTime1900.Unix(), makeDate(sy, time.Month(sm), sd))
		diff = s - e
	}
	return diff
}

// DATEDIF function calculates the number of days, months, or years between
// two dates. The syntax of the function is:
//
//	DATEDIF(start_date,end_date,unit)
func (fn *formulaFuncs) DATEDIF(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "DATEDIF requires 3 number arguments")
	}
	startArg, endArg := argsList.Front().Value.(formulaArg).ToNumber(), argsList.Front().Next().Value.(formulaArg).ToNumber()
	if startArg.Type != ArgNumber || endArg.Type != ArgNumber {
		return startArg
	}
	if startArg.Number > endArg.Number {
		return newErrorFormulaArg(formulaErrorNUM, "start_date > end_date")
	}
	if startArg.Number == endArg.Number {
		return newNumberFormulaArg(0)
	}
	unit := strings.ToLower(argsList.Back().Value.(formulaArg).Value())
	startDate, endDate := timeFromExcelTime(startArg.Number, false), timeFromExcelTime(endArg.Number, false)
	sy, smm, sd := startDate.Date()
	ey, emm, ed := endDate.Date()
	sm, em, diff := int(smm), int(emm), 0.0
	switch unit {
	case "y":
		diff = float64(ey - sy)
		if em < sm || (em == sm && ed < sd) {
			diff--
		}
	case "m":
		yDiff := ey - sy
		mDiff := em - sm
		if ed < sd {
			mDiff--
		}
		if mDiff < 0 {
			yDiff--
			mDiff += 12
		}
		diff = float64(yDiff*12 + mDiff)
	case "d", "md", "ym", "yd":
		diff = calcDateDif(unit, diff, []int{ey, sy, em, sm, ed, sd}, startArg, endArg)
	default:
		return newErrorFormulaArg(formulaErrorVALUE, "DATEDIF has invalid unit")
	}
	return newNumberFormulaArg(diff)
}

// isDateOnlyFmt check if the given string matches date-only format regular expressions.
func isDateOnlyFmt(dateString string) bool {
	for _, df := range dateOnlyFormats {
		subMatch := df.FindStringSubmatch(dateString)
		if len(subMatch) > 1 {
			return true
		}
	}
	return false
}

// isTimeOnlyFmt check if the given string matches time-only format regular expressions.
func isTimeOnlyFmt(timeString string) bool {
	for _, tf := range timeFormats {
		subMatch := tf.FindStringSubmatch(timeString)
		if len(subMatch) > 1 {
			return true
		}
	}
	return false
}

// strToTimePatternHandler1 parse and convert the given string in pattern
// hh to the time.
func strToTimePatternHandler1(subMatch []string) (h, m int, s float64, err error) {
	h, err = strconv.Atoi(subMatch[0])
	return
}

// strToTimePatternHandler2 parse and convert the given string in pattern
// hh:mm to the time.
func strToTimePatternHandler2(subMatch []string) (h, m int, s float64, err error) {
	if h, err = strconv.Atoi(subMatch[0]); err != nil {
		return
	}
	m, err = strconv.Atoi(subMatch[2])
	return
}

// strToTimePatternHandler3 parse and convert the given string in pattern
// mm:ss to the time.
func strToTimePatternHandler3(subMatch []string) (h, m int, s float64, err error) {
	if m, err = strconv.Atoi(subMatch[0]); err != nil {
		return
	}
	s, err = strconv.ParseFloat(subMatch[2], 64)
	return
}

// strToTimePatternHandler4 parse and convert the given string in pattern
// hh:mm:ss to the time.
func strToTimePatternHandler4(subMatch []string) (h, m int, s float64, err error) {
	if h, err = strconv.Atoi(subMatch[0]); err != nil {
		return
	}
	if m, err = strconv.Atoi(subMatch[2]); err != nil {
		return
	}
	s, err = strconv.ParseFloat(subMatch[4], 64)
	return
}

// strToTime parse and convert the given string to the time.
func strToTime(str string) (int, int, float64, bool, bool, formulaArg) {
	var subMatch []string
	pattern := ""
	for key, tf := range timeFormats {
		subMatch = tf.FindStringSubmatch(str)
		if len(subMatch) > 1 {
			pattern = key
			break
		}
	}
	if pattern == "" {
		return 0, 0, 0, false, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	dateIsEmpty := subMatch[1] == ""
	subMatch = subMatch[49:]
	var (
		l              = len(subMatch)
		last           = subMatch[l-1]
		am             = last == "am"
		pm             = last == "pm"
		hours, minutes int
		seconds        float64
		err            error
	)
	if handler, ok := map[string]func(match []string) (int, int, float64, error){
		"hh":       strToTimePatternHandler1,
		"hh:mm":    strToTimePatternHandler2,
		"mm:ss":    strToTimePatternHandler3,
		"hh:mm:ss": strToTimePatternHandler4,
	}[pattern]; ok {
		if hours, minutes, seconds, err = handler(subMatch); err != nil {
			return 0, 0, 0, false, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	if minutes >= 60 {
		return 0, 0, 0, false, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if am || pm {
		if hours > 12 || seconds >= 60 {
			return 0, 0, 0, false, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		} else if hours == 12 {
			hours = 0
		}
	} else if hours >= 24 || seconds >= 10000 {
		return 0, 0, 0, false, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return hours, minutes, seconds, pm, dateIsEmpty, newEmptyFormulaArg()
}

// strToDatePatternHandler1 parse and convert the given string in pattern
// mm/dd/yy to the date.
func strToDatePatternHandler1(subMatch []string) (int, int, int, bool, error) {
	var year, month, day int
	var err error
	if month, err = strconv.Atoi(subMatch[1]); err != nil {
		return 0, 0, 0, false, err
	}
	if day, err = strconv.Atoi(subMatch[3]); err != nil {
		return 0, 0, 0, false, err
	}
	if year, err = strconv.Atoi(subMatch[5]); err != nil {
		return 0, 0, 0, false, err
	}
	if year < 0 || year > 9999 || (year > 99 && year < 1900) {
		return 0, 0, 0, false, ErrParameterInvalid
	}
	return formatYear(year), month, day, subMatch[8] == "", err
}

// strToDatePatternHandler2 parse and convert the given string in pattern mm
// dd, yy to the date.
func strToDatePatternHandler2(subMatch []string) (int, int, int, bool, error) {
	var year, month, day int
	var err error
	month = month2num[subMatch[1]]
	if day, err = strconv.Atoi(subMatch[14]); err != nil {
		return 0, 0, 0, false, err
	}
	if year, err = strconv.Atoi(subMatch[16]); err != nil {
		return 0, 0, 0, false, err
	}
	if year < 0 || year > 9999 || (year > 99 && year < 1900) {
		return 0, 0, 0, false, ErrParameterInvalid
	}
	return formatYear(year), month, day, subMatch[19] == "", err
}

// strToDatePatternHandler3 parse and convert the given string in pattern
// yy-mm-dd to the date.
func strToDatePatternHandler3(subMatch []string) (int, int, int, bool, error) {
	var year, month, day int
	v1, err := strconv.Atoi(subMatch[1])
	if err != nil {
		return 0, 0, 0, false, err
	}
	v2, err := strconv.Atoi(subMatch[3])
	if err != nil {
		return 0, 0, 0, false, err
	}
	v3, err := strconv.Atoi(subMatch[5])
	if err != nil {
		return 0, 0, 0, false, err
	}
	if v1 >= 1900 && v1 < 10000 {
		year = v1
		month = v2
		day = v3
	} else if v1 > 0 && v1 < 13 {
		month = v1
		day = v2
		year = v3
	} else {
		return 0, 0, 0, false, ErrParameterInvalid
	}
	return year, month, day, subMatch[8] == "", err
}

// strToDatePatternHandler4 parse and convert the given string in pattern
// yy-mmStr-dd, yy to the date.
func strToDatePatternHandler4(subMatch []string) (int, int, int, bool, error) {
	var year, month, day int
	var err error
	if year, err = strconv.Atoi(subMatch[16]); err != nil {
		return 0, 0, 0, false, err
	}
	month = month2num[subMatch[3]]
	if day, err = strconv.Atoi(subMatch[1]); err != nil {
		return 0, 0, 0, false, err
	}
	return formatYear(year), month, day, subMatch[19] == "", err
}

// strToDate parse and convert the given string to the date.
func strToDate(str string) (int, int, int, bool, formulaArg) {
	var subMatch []string
	pattern := ""
	for key, df := range dateFormats {
		subMatch = df.FindStringSubmatch(str)
		if len(subMatch) > 1 {
			pattern = key
			break
		}
	}
	if pattern == "" {
		return 0, 0, 0, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	var (
		timeIsEmpty      bool
		year, month, day int
		err              error
	)
	if handler, ok := map[string]func(match []string) (int, int, int, bool, error){
		"mm/dd/yy":    strToDatePatternHandler1,
		"mm dd, yy":   strToDatePatternHandler2,
		"yy-mm-dd":    strToDatePatternHandler3,
		"yy-mmStr-dd": strToDatePatternHandler4,
	}[pattern]; ok {
		if year, month, day, timeIsEmpty, err = handler(subMatch); err != nil {
			return 0, 0, 0, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	if !validateDate(year, month, day) {
		return 0, 0, 0, false, newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return year, month, day, timeIsEmpty, newEmptyFormulaArg()
}

// DATEVALUE function converts a text representation of a date into an Excel
// date. For example, the function converts a text string representing a
// date, into the serial number that represents the date in Excels' date-time
// code. The syntax of the function is:
//
//	DATEVALUE(date_text)
func (fn *formulaFuncs) DATEVALUE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "DATEVALUE requires 1 argument")
	}
	dateText := argsList.Front().Value.(formulaArg).Value()
	if !isDateOnlyFmt(dateText) {
		if _, _, _, _, _, err := strToTime(dateText); err.Type == ArgError {
			return err
		}
	}
	y, m, d, _, err := strToDate(dateText)
	if err.Type == ArgError {
		return err
	}
	return newNumberFormulaArg(daysBetween(excelMinTime1900.Unix(), makeDate(y, time.Month(m), d)) + 1)
}

// DAY function returns the day of a date, represented by a serial number. The
// day is given as an integer ranging from 1 to 31. The syntax of the
// function is:
//
//	DAY(serial_number)
func (fn *formulaFuncs) DAY(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "DAY requires exactly 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg)
	num := arg.ToNumber()
	if num.Type != ArgNumber {
		dateString := strings.ToLower(arg.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		_, _, day, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		return newNumberFormulaArg(float64(day))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "DAY only accepts positive argument")
	}
	if num.Number <= 60 {
		return newNumberFormulaArg(math.Mod(num.Number, 31.0))
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Day()))
}

// DAYS function returns the number of days between two supplied dates. The
// syntax of the function is:
//
//	DAYS(end_date,start_date)
func (fn *formulaFuncs) DAYS(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "DAYS requires 2 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	end, start := args.List[0], args.List[1]
	return newNumberFormulaArg(end.Number - start.Number)
}

// DAYS360 function returns the number of days between 2 dates, based on a
// 360-day year (12 x 30 months). The syntax of the function is:
//
//	DAYS360(start_date,end_date,[method])
func (fn *formulaFuncs) DAYS360(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "DAYS360 requires at least 2 arguments")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "DAYS360 requires at most 3 arguments")
	}
	startDate := toExcelDateArg(argsList.Front().Value.(formulaArg))
	if startDate.Type != ArgNumber {
		return startDate
	}
	endDate := toExcelDateArg(argsList.Front().Next().Value.(formulaArg))
	if endDate.Type != ArgNumber {
		return endDate
	}
	start, end := timeFromExcelTime(startDate.Number, false), timeFromExcelTime(endDate.Number, false)
	sy, sm, sd, ey, em, ed := start.Year(), int(start.Month()), start.Day(), end.Year(), int(end.Month()), end.Day()
	method := newBoolFormulaArg(false)
	if argsList.Len() > 2 {
		if method = argsList.Back().Value.(formulaArg).ToBool(); method.Type != ArgNumber {
			return method
		}
	}
	if method.Number == 1 {
		if sd == 31 {
			sd--
		}
		if ed == 31 {
			ed--
		}
	} else {
		if getDaysInMonth(sy, sm) == sd {
			sd = 30
		}
		if ed > 30 {
			if sd < 30 {
				em++
				ed = 1
			} else {
				ed = 30
			}
		}
	}
	return newNumberFormulaArg(float64(360*(ey-sy) + 30*(em-sm) + (ed - sd)))
}

// ISOWEEKNUM function returns the ISO week number of a supplied date. The
// syntax of the function is:
//
//	ISOWEEKNUM(date)
func (fn *formulaFuncs) ISOWEEKNUM(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISOWEEKNUM requires 1 argument")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	weekNum := 0
	if num.Type != ArgNumber {
		dateString := strings.ToLower(date.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		_, weekNum = time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).ISOWeek()
	} else {
		if num.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		_, weekNum = timeFromExcelTime(num.Number, false).ISOWeek()
	}
	return newNumberFormulaArg(float64(weekNum))
}

// EDATE function returns a date that is a specified number of months before or
// after a supplied start date. The syntax of function is:
//
//	EDATE(start_date,months)
func (fn *formulaFuncs) EDATE(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "EDATE requires 2 arguments")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	var dateTime time.Time
	if num.Type != ArgNumber {
		dateString := strings.ToLower(date.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		dateTime = time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Now().Location())
	} else {
		if num.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		dateTime = timeFromExcelTime(num.Number, false)
	}
	month := argsList.Back().Value.(formulaArg).ToNumber()
	if month.Type != ArgNumber {
		return month
	}
	y, d := dateTime.Year(), dateTime.Day()
	m := int(dateTime.Month()) + int(month.Number)
	if month.Number < 0 {
		y -= int(math.Ceil(-1 * float64(m) / 12))
	}
	if month.Number > 11 {
		y += int(math.Floor(float64(m) / 12))
	}
	if m = m % 12; m < 0 {
		m += 12
	}
	if d > 28 {
		if days := getDaysInMonth(y, m); d > days {
			d = days
		}
	}
	result, _ := timeToExcelTime(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), false)
	return newNumberFormulaArg(result)
}

// EOMONTH function returns the last day of the month, that is a specified
// number of months before or after an initial supplied start date. The syntax
// of the function is:
//
//	EOMONTH(start_date,months)
func (fn *formulaFuncs) EOMONTH(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "EOMONTH requires 2 arguments")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	var dateTime time.Time
	if num.Type != ArgNumber {
		dateString := strings.ToLower(date.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		dateTime = time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Now().Location())
	} else {
		if num.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		dateTime = timeFromExcelTime(num.Number, false)
	}
	months := argsList.Back().Value.(formulaArg).ToNumber()
	if months.Type != ArgNumber {
		return months
	}
	y, m := dateTime.Year(), int(dateTime.Month())+int(months.Number)-1
	if m < 0 {
		y -= int(math.Ceil(-1 * float64(m) / 12))
	}
	if m > 11 {
		y += int(math.Floor(float64(m) / 12))
	}
	if m = m % 12; m < 0 {
		m += 12
	}
	result, _ := timeToExcelTime(time.Date(y, time.Month(m+1), getDaysInMonth(y, m+1), 0, 0, 0, 0, time.UTC), false)
	return newNumberFormulaArg(result)
}

// HOUR function returns an integer representing the hour component of a
// supplied Excel time. The syntax of the function is:
//
//	HOUR(serial_number)
func (fn *formulaFuncs) HOUR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HOUR requires exactly 1 argument")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	if num.Type != ArgNumber {
		timeString := strings.ToLower(date.Value())
		if !isTimeOnlyFmt(timeString) {
			_, _, _, _, err := strToDate(timeString)
			if err.Type == ArgError {
				return err
			}
		}
		h, _, _, pm, _, err := strToTime(timeString)
		if err.Type == ArgError {
			return err
		}
		if pm {
			h += 12
		}
		return newNumberFormulaArg(float64(h))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "HOUR only accepts positive argument")
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Hour()))
}

// MINUTE function returns an integer representing the minute component of a
// supplied Excel time. The syntax of the function is:
//
//	MINUTE(serial_number)
func (fn *formulaFuncs) MINUTE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MINUTE requires exactly 1 argument")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	if num.Type != ArgNumber {
		timeString := strings.ToLower(date.Value())
		if !isTimeOnlyFmt(timeString) {
			_, _, _, _, err := strToDate(timeString)
			if err.Type == ArgError {
				return err
			}
		}
		_, m, _, _, _, err := strToTime(timeString)
		if err.Type == ArgError {
			return err
		}
		return newNumberFormulaArg(float64(m))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "MINUTE only accepts positive argument")
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Minute()))
}

// MONTH function returns the month of a date represented by a serial number.
// The month is given as an integer, ranging from 1 (January) to 12
// (December). The syntax of the function is:
//
//	MONTH(serial_number)
func (fn *formulaFuncs) MONTH(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "MONTH requires exactly 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg)
	num := arg.ToNumber()
	if num.Type != ArgNumber {
		dateString := strings.ToLower(arg.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		_, month, _, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		return newNumberFormulaArg(float64(month))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "MONTH only accepts positive argument")
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Month()))
}

// genWeekendMask generate weekend mask of a series of seven 0's and 1's which
// represent the seven weekdays, starting from Monday.
func genWeekendMask(weekend int) []byte {
	if masks, ok := map[int][]int{
		1: {5, 6}, 2: {6, 0}, 3: {0, 1}, 4: {1, 2}, 5: {2, 3}, 6: {3, 4}, 7: {4, 5},
		11: {6}, 12: {0}, 13: {1}, 14: {2}, 15: {3}, 16: {4}, 17: {5},
	}[weekend]; ok {
		mask := make([]byte, 7)
		for _, idx := range masks {
			mask[idx] = 1
		}
		return mask
	}
	return nil
}

// isWorkday check if the date is workday.
func isWorkday(weekendMask []byte, date float64) bool {
	dateTime := timeFromExcelTime(date, false)
	weekday := dateTime.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	return weekendMask[weekday-1] == 0
}

// prepareWorkday returns weekend mask and workdays pre week by given days
// counted as weekend.
func prepareWorkday(weekend formulaArg) ([]byte, int) {
	weekendArg := weekend.ToNumber()
	if weekendArg.Type != ArgNumber {
		return nil, 0
	}
	var weekendMask []byte
	var workdaysPerWeek int
	if len(weekend.Value()) == 7 {
		// possible string values for the weekend argument
		for _, mask := range weekend.Value() {
			if mask != '0' && mask != '1' {
				return nil, 0
			}
			weekendMask = append(weekendMask, byte(mask)-48)
		}
	} else {
		weekendMask = genWeekendMask(int(weekendArg.Number))
	}
	for _, mask := range weekendMask {
		if mask == 0 {
			workdaysPerWeek++
		}
	}
	return weekendMask, workdaysPerWeek
}

// toExcelDateArg function converts a text representation of a time, into an
// Excel date time number formula argument.
func toExcelDateArg(arg formulaArg) formulaArg {
	num := arg.ToNumber()
	if num.Type != ArgNumber {
		dateString := strings.ToLower(arg.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		num.Number, _ = timeToExcelTime(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), false)
		return newNumberFormulaArg(num.Number)
	}
	if arg.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return num
}

// prepareHolidays function converts array type formula arguments to into an
// Excel date time number formula arguments list.
func prepareHolidays(args formulaArg) []int {
	var holidays []int
	for _, arg := range args.ToList() {
		num := toExcelDateArg(arg)
		if num.Type != ArgNumber {
			continue
		}
		holidays = append(holidays, int(math.Ceil(num.Number)))
	}
	return holidays
}

// workdayIntl is an implementation of the formula function WORKDAY.INTL.
func workdayIntl(endDate, sign int, holidays []int, weekendMask []byte, startDate float64) int {
	for i := 0; i < len(holidays); i++ {
		holiday := holidays[i]
		if sign > 0 {
			if holiday > endDate {
				break
			}
		} else {
			if holiday < endDate {
				break
			}
		}
		if sign > 0 {
			if holiday > int(math.Ceil(startDate)) {
				if isWorkday(weekendMask, float64(holiday)) {
					endDate += sign
					for !isWorkday(weekendMask, float64(endDate)) {
						endDate += sign
					}
				}
			}
		} else {
			if holiday < int(math.Ceil(startDate)) {
				if isWorkday(weekendMask, float64(holiday)) {
					endDate += sign
					for !isWorkday(weekendMask, float64(endDate)) {
						endDate += sign
					}
				}
			}
		}
	}
	return endDate
}

// NETWORKDAYS function calculates the number of work days between two supplied
// dates (including the start and end date). The calculation includes all
// weekdays (Mon - Fri), excluding a supplied list of holidays. The syntax of
// the function is:
//
//	NETWORKDAYS(start_date,end_date,[holidays])
func (fn *formulaFuncs) NETWORKDAYS(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "NETWORKDAYS requires at least 2 arguments")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "NETWORKDAYS requires at most 3 arguments")
	}
	args := list.New()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(argsList.Front().Next().Value.(formulaArg))
	args.PushBack(newNumberFormulaArg(1))
	if argsList.Len() == 3 {
		args.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.NETWORKDAYSdotINTL(args)
}

// NETWORKDAYSdotINTL function calculates the number of whole work days between
// two supplied dates, excluding weekends and holidays. The function allows
// the user to specify which days are counted as weekends and holidays. The
// syntax of the function is:
//
//	NETWORKDAYS.INTL(start_date,end_date,[weekend],[holidays])
func (fn *formulaFuncs) NETWORKDAYSdotINTL(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "NETWORKDAYS.INTL requires at least 2 arguments")
	}
	if argsList.Len() > 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "NETWORKDAYS.INTL requires at most 4 arguments")
	}
	startDate := toExcelDateArg(argsList.Front().Value.(formulaArg))
	if startDate.Type != ArgNumber {
		return startDate
	}
	endDate := toExcelDateArg(argsList.Front().Next().Value.(formulaArg))
	if endDate.Type != ArgNumber {
		return endDate
	}
	weekend := newNumberFormulaArg(1)
	if argsList.Len() > 2 {
		weekend = argsList.Front().Next().Next().Value.(formulaArg)
	}
	var holidays []int
	if argsList.Len() == 4 {
		holidays = prepareHolidays(argsList.Back().Value.(formulaArg))
		sort.Ints(holidays)
	}
	weekendMask, workdaysPerWeek := prepareWorkday(weekend)
	if workdaysPerWeek == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	sign := 1
	if startDate.Number > endDate.Number {
		sign = -1
		temp := startDate.Number
		startDate.Number = endDate.Number
		endDate.Number = temp
	}
	offset := endDate.Number - startDate.Number
	count := int(math.Floor(offset/7) * float64(workdaysPerWeek))
	daysMod := int(offset) % 7
	for daysMod >= 0 {
		if isWorkday(weekendMask, endDate.Number-float64(daysMod)) {
			count++
		}
		daysMod--
	}
	for i := 0; i < len(holidays); i++ {
		holiday := float64(holidays[i])
		if isWorkday(weekendMask, holiday) && holiday >= startDate.Number && holiday <= endDate.Number {
			count--
		}
	}
	return newNumberFormulaArg(float64(sign * count))
}

// WORKDAY function returns a date that is a supplied number of working days
// (excluding weekends and holidays) ahead of a given start date. The syntax
// of the function is:
//
//	WORKDAY(start_date,days,[holidays])
func (fn *formulaFuncs) WORKDAY(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "WORKDAY requires at least 2 arguments")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "WORKDAY requires at most 3 arguments")
	}
	args := list.New()
	args.PushBack(argsList.Front().Value.(formulaArg))
	args.PushBack(argsList.Front().Next().Value.(formulaArg))
	args.PushBack(newNumberFormulaArg(1))
	if argsList.Len() == 3 {
		args.PushBack(argsList.Back().Value.(formulaArg))
	}
	return fn.WORKDAYdotINTL(args)
}

// WORKDAYdotINTL function returns a date that is a supplied number of working
// days (excluding weekends and holidays) ahead of a given start date. The
// function allows the user to specify which days of the week are counted as
// weekends. The syntax of the function is:
//
//	WORKDAY.INTL(start_date,days,[weekend],[holidays])
func (fn *formulaFuncs) WORKDAYdotINTL(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "WORKDAY.INTL requires at least 2 arguments")
	}
	if argsList.Len() > 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "WORKDAY.INTL requires at most 4 arguments")
	}
	startDate := toExcelDateArg(argsList.Front().Value.(formulaArg))
	if startDate.Type != ArgNumber {
		return startDate
	}
	days := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if days.Type != ArgNumber {
		return days
	}
	weekend := newNumberFormulaArg(1)
	if argsList.Len() > 2 {
		weekend = argsList.Front().Next().Next().Value.(formulaArg)
	}
	var holidays []int
	if argsList.Len() == 4 {
		holidays = prepareHolidays(argsList.Back().Value.(formulaArg))
		sort.Ints(holidays)
	}
	if days.Number == 0 {
		return newNumberFormulaArg(math.Ceil(startDate.Number))
	}
	weekendMask, workdaysPerWeek := prepareWorkday(weekend)
	if workdaysPerWeek == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	sign := 1
	if days.Number < 0 {
		sign = -1
	}
	offset := int(days.Number) / workdaysPerWeek
	daysMod := int(days.Number) % workdaysPerWeek
	endDate := int(math.Ceil(startDate.Number)) + offset*7
	if daysMod == 0 {
		for !isWorkday(weekendMask, float64(endDate)) {
			endDate -= sign
		}
	} else {
		for daysMod != 0 {
			endDate += sign
			if isWorkday(weekendMask, float64(endDate)) {
				if daysMod < 0 {
					daysMod++
					continue
				}
				daysMod--
			}
		}
	}
	return newNumberFormulaArg(float64(workdayIntl(endDate, sign, holidays, weekendMask, startDate.Number)))
}

// YEAR function returns an integer representing the year of a supplied date.
// The syntax of the function is:
//
//	YEAR(serial_number)
func (fn *formulaFuncs) YEAR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "YEAR requires exactly 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg)
	num := arg.ToNumber()
	if num.Type != ArgNumber {
		dateString := strings.ToLower(arg.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		year, _, _, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		return newNumberFormulaArg(float64(year))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "YEAR only accepts positive argument")
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Year()))
}

// yearFracBasisCond is an implementation of the yearFracBasis1.
func yearFracBasisCond(sy, sm, sd, ey, em, ed int) bool {
	return (isLeapYear(sy) && (sm < 2 || (sm == 2 && sd <= 29))) || (isLeapYear(ey) && (em > 2 || (em == 2 && ed == 29)))
}

// yearFracBasis0 function returns the fraction of a year that between two
// supplied dates in US (NASD) 30/360 type of day.
func yearFracBasis0(startDate, endDate float64) (dayDiff, daysInYear float64) {
	startTime, endTime := timeFromExcelTime(startDate, false), timeFromExcelTime(endDate, false)
	sy, smM, sd := startTime.Date()
	ey, emM, ed := endTime.Date()
	sm, em := int(smM), int(emM)
	if sd == 31 {
		sd--
	}
	if sd == 30 && ed == 31 {
		ed--
	} else if leap := isLeapYear(sy); sm == 2 && ((leap && sd == 29) || (!leap && sd == 28)) {
		sd = 30
		if leap := isLeapYear(ey); em == 2 && ((leap && ed == 29) || (!leap && ed == 28)) {
			ed = 30
		}
	}
	dayDiff = float64((ey-sy)*360 + (em-sm)*30 + (ed - sd))
	daysInYear = 360
	return
}

// yearFracBasis1 function returns the fraction of a year that between two
// supplied dates in actual type of day.
func yearFracBasis1(startDate, endDate float64) (dayDiff, daysInYear float64) {
	startTime, endTime := timeFromExcelTime(startDate, false), timeFromExcelTime(endDate, false)
	sy, smM, sd := startTime.Date()
	ey, emM, ed := endTime.Date()
	sm, em := int(smM), int(emM)
	dayDiff = endDate - startDate
	isYearDifferent := sy != ey
	if isYearDifferent && (ey != sy+1 || sm < em || (sm == em && sd < ed)) {
		dayCount := 0
		for y := sy; y <= ey; y++ {
			dayCount += getYearDays(y, 1)
		}
		daysInYear = float64(dayCount) / float64(ey-sy+1)
	} else {
		if !isYearDifferent && isLeapYear(sy) {
			daysInYear = 366
		} else {
			if isYearDifferent && yearFracBasisCond(sy, sm, sd, ey, em, ed) {
				daysInYear = 366
			} else {
				daysInYear = 365
			}
		}
	}
	return
}

// yearFracBasis4 function returns the fraction of a year that between two
// supplied dates in European 30/360 type of day.
func yearFracBasis4(startDate, endDate float64) (dayDiff, daysInYear float64) {
	startTime, endTime := timeFromExcelTime(startDate, false), timeFromExcelTime(endDate, false)
	sy, smM, sd := startTime.Date()
	ey, emM, ed := endTime.Date()
	sm, em := int(smM), int(emM)
	if sd == 31 {
		sd--
	}
	if ed == 31 {
		ed--
	}
	dayDiff = float64((ey-sy)*360 + (em-sm)*30 + (ed - sd))
	daysInYear = 360
	return
}

// yearFrac is an implementation of the formula function YEARFRAC.
func yearFrac(startDate, endDate float64, basis int) formulaArg {
	startTime, endTime := timeFromExcelTime(startDate, false), timeFromExcelTime(endDate, false)
	if startTime == endTime {
		return newNumberFormulaArg(0)
	}
	var dayDiff, daysInYear float64
	switch basis {
	case 0:
		dayDiff, daysInYear = yearFracBasis0(startDate, endDate)
	case 1:
		dayDiff, daysInYear = yearFracBasis1(startDate, endDate)
	case 2:
		dayDiff = endDate - startDate
		daysInYear = 360
	case 3:
		dayDiff = endDate - startDate
		daysInYear = 365
	case 4:
		dayDiff, daysInYear = yearFracBasis4(startDate, endDate)
	default:
		return newErrorFormulaArg(formulaErrorNUM, "invalid basis")
	}
	return newNumberFormulaArg(dayDiff / daysInYear)
}

// getYearDays return days of the year with specifying the type of day count
// basis to be used.
func getYearDays(year, basis int) int {
	switch basis {
	case 1:
		if isLeapYear(year) {
			return 366
		}
		return 365
	case 3:
		return 365
	default:
		return 360
	}
}

// YEARFRAC function returns the fraction of a year that is represented by the
// number of whole days between two supplied dates. The syntax of the
// function is:
//
//	YEARFRAC(start_date,end_date,[basis])
func (fn *formulaFuncs) YEARFRAC(argsList *list.List) formulaArg {
	if argsList.Len() != 2 && argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "YEARFRAC requires 3 or 4 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	start, end := args.List[0], args.List[1]
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 3 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return basis
		}
	}
	return yearFrac(start.Number, end.Number, int(basis.Number))
}

// NOW function returns the current date and time. The function receives no
// arguments and therefore. The syntax of the function is:
//
//	NOW()
func (fn *formulaFuncs) NOW(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "NOW accepts no arguments")
	}
	now := time.Now()
	_, offset := now.Zone()
	return newNumberFormulaArg(25569.0 + float64(now.Unix()+int64(offset))/86400)
}

// SECOND function returns an integer representing the second component of a
// supplied Excel time. The syntax of the function is:
//
//	SECOND(serial_number)
func (fn *formulaFuncs) SECOND(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "SECOND requires exactly 1 argument")
	}
	date := argsList.Front().Value.(formulaArg)
	num := date.ToNumber()
	if num.Type != ArgNumber {
		timeString := strings.ToLower(date.Value())
		if !isTimeOnlyFmt(timeString) {
			_, _, _, _, err := strToDate(timeString)
			if err.Type == ArgError {
				return err
			}
		}
		_, _, s, _, _, err := strToTime(timeString)
		if err.Type == ArgError {
			return err
		}
		return newNumberFormulaArg(float64(int(s) % 60))
	}
	if num.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "SECOND only accepts positive argument")
	}
	return newNumberFormulaArg(float64(timeFromExcelTime(num.Number, false).Second()))
}

// TIME function accepts three integer arguments representing hours, minutes
// and seconds, and returns an Excel time. I.e. the function returns the
// decimal value that represents the time in Excel. The syntax of the
// function is:
//
//	TIME(hour,minute,second)
func (fn *formulaFuncs) TIME(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TIME requires 3 number arguments")
	}
	h := argsList.Front().Value.(formulaArg).ToNumber()
	m := argsList.Front().Next().Value.(formulaArg).ToNumber()
	s := argsList.Back().Value.(formulaArg).ToNumber()
	if h.Type != ArgNumber || m.Type != ArgNumber || s.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, "TIME requires 3 number arguments")
	}
	t := (h.Number*3600 + m.Number*60 + s.Number) / 86400
	if t < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(t)
}

// TIMEVALUE function converts a text representation of a time, into an Excel
// time. The syntax of the function is:
//
//	TIMEVALUE(time_text)
func (fn *formulaFuncs) TIMEVALUE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TIMEVALUE requires exactly 1 argument")
	}
	date := argsList.Front().Value.(formulaArg)
	timeString := strings.ToLower(date.Value())
	if !isTimeOnlyFmt(timeString) {
		_, _, _, _, err := strToDate(timeString)
		if err.Type == ArgError {
			return err
		}
	}
	h, m, s, pm, _, err := strToTime(timeString)
	if err.Type == ArgError {
		return err
	}
	if pm {
		h += 12
	}
	args := list.New()
	args.PushBack(newNumberFormulaArg(float64(h)))
	args.PushBack(newNumberFormulaArg(float64(m)))
	args.PushBack(newNumberFormulaArg(s))
	return fn.TIME(args)
}

// TODAY function returns the current date. The function has no arguments and
// therefore. The syntax of the function is:
//
//	TODAY()
func (fn *formulaFuncs) TODAY(argsList *list.List) formulaArg {
	if argsList.Len() != 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "TODAY accepts no arguments")
	}
	now := time.Now()
	_, offset := now.Zone()
	return newNumberFormulaArg(daysBetween(excelMinTime1900.Unix(), now.Unix()+int64(offset)) + 1)
}

// makeDate return date as a Unix time, the number of seconds elapsed since
// January 1, 1970 UTC.
func makeDate(y int, m time.Month, d int) int64 {
	if y == 1900 && int(m) <= 2 {
		d--
	}
	date := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return date.Unix()
}

// daysBetween return time interval of the given start timestamp and end
// timestamp.
func daysBetween(startDate, endDate int64) float64 {
	return float64(int(0.5 + float64((endDate-startDate)/86400)))
}

// WEEKDAY function returns an integer representing the day of the week for a
// supplied date. The syntax of the function is:
//
//	WEEKDAY(serial_number,[return_type])
func (fn *formulaFuncs) WEEKDAY(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEEKDAY requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEEKDAY allows at most 2 arguments")
	}
	sn := argsList.Front().Value.(formulaArg)
	num := sn.ToNumber()
	weekday, returnType := 0, 1
	if num.Type != ArgNumber {
		dateString := strings.ToLower(sn.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		weekday = int(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Now().Location()).Weekday())
	} else {
		if num.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		weekday = int(timeFromExcelTime(num.Number, false).Weekday())
	}
	if argsList.Len() == 2 {
		returnTypeArg := argsList.Back().Value.(formulaArg).ToNumber()
		if returnTypeArg.Type != ArgNumber {
			return returnTypeArg
		}
		returnType = int(returnTypeArg.Number)
	}
	if returnType == 2 {
		returnType = 11
	}
	weekday++
	if returnType == 1 {
		return newNumberFormulaArg(float64(weekday))
	}
	if returnType == 3 {
		return newNumberFormulaArg(float64((weekday + 6 - 1) % 7))
	}
	if returnType >= 11 && returnType <= 17 {
		return newNumberFormulaArg(float64((weekday+6-(returnType-10))%7 + 1))
	}
	return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
}

// weeknum is an implementation of the formula function WEEKNUM.
func (fn *formulaFuncs) weeknum(snTime time.Time, returnType int) formulaArg {
	days := snTime.YearDay()
	weekMod, weekNum := days%7, math.Ceil(float64(days)/7)
	if weekMod == 0 {
		weekMod = 7
	}
	year := snTime.Year()
	firstWeekday := int(time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday())
	var offset int
	switch returnType {
	case 1, 17:
		offset = 0
	case 2, 11, 21:
		offset = 1
	case 12, 13, 14, 15, 16:
		offset = returnType - 10
	default:
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	padding := offset + 7 - firstWeekday
	if padding > 7 {
		padding -= 7
	}
	if weekMod > padding {
		weekNum++
	}
	if returnType == 21 && (firstWeekday == 0 || firstWeekday > 4) {
		if weekNum--; weekNum < 1 {
			if weekNum = 52; int(time.Date(year-1, time.January, 1, 0, 0, 0, 0, time.UTC).Weekday()) < 4 {
				weekNum++
			}
		}
	}
	return newNumberFormulaArg(weekNum)
}

// WEEKNUM function returns an integer representing the week number (from 1 to
// 53) of the year. The syntax of the function is:
//
//	WEEKNUM(serial_number,[return_type])
func (fn *formulaFuncs) WEEKNUM(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEEKNUM requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "WEEKNUM allows at most 2 arguments")
	}
	sn := argsList.Front().Value.(formulaArg)
	num, returnType := sn.ToNumber(), 1
	var snTime time.Time
	if num.Type != ArgNumber {
		dateString := strings.ToLower(sn.Value())
		if !isDateOnlyFmt(dateString) {
			if _, _, _, _, _, err := strToTime(dateString); err.Type == ArgError {
				return err
			}
		}
		y, m, d, _, err := strToDate(dateString)
		if err.Type == ArgError {
			return err
		}
		snTime = time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.Now().Location())
	} else {
		if num.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		snTime = timeFromExcelTime(num.Number, false)
	}
	if argsList.Len() == 2 {
		returnTypeArg := argsList.Back().Value.(formulaArg).ToNumber()
		if returnTypeArg.Type != ArgNumber {
			return returnTypeArg
		}
		returnType = int(returnTypeArg.Number)
	}
	return fn.weeknum(snTime, returnType)
}

// Text Functions

// prepareToText checking and prepare arguments for the formula functions
// ARRAYTOTEXT and VALUETOTEXT.
func prepareToText(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 2 arguments", name))
	}
	format := newNumberFormulaArg(0)
	if argsList.Len() == 2 {
		if format = argsList.Back().Value.(formulaArg).ToNumber(); format.Type != ArgNumber {
			return format
		}
	}
	if format.Number != 0 && format.Number != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return format
}

// ARRAYTOTEXT function returns an array of text values from any specified
// range. It passes text values unchanged, and converts non-text values to
// text. The syntax of the function is:
//
//	ARRAYTOTEXT(array,[format])
func (fn *formulaFuncs) ARRAYTOTEXT(argsList *list.List) formulaArg {
	var mtx [][]string
	format := prepareToText("ARRAYTOTEXT", argsList)
	if format.Type != ArgNumber {
		return format
	}
	for _, rows := range argsList.Front().Value.(formulaArg).Matrix {
		var row []string
		for _, cell := range rows {
			if num := cell.ToNumber(); num.Type != ArgNumber && format.Number == 1 {
				row = append(row, fmt.Sprintf("\"%s\"", cell.Value()))
				continue
			}
			row = append(row, cell.Value())
		}
		mtx = append(mtx, row)
	}
	var text []string
	for _, row := range mtx {
		if format.Number == 1 {
			text = append(text, strings.Join(row, ","))
			continue
		}
		text = append(text, strings.Join(row, ", "))
	}
	if format.Number == 1 {
		return newStringFormulaArg(fmt.Sprintf("{%s}", strings.Join(text, ";")))
	}
	return newStringFormulaArg(strings.Join(text, ", "))
}

// CHAR function returns the character relating to a supplied character set
// number (from 1 to 255). The syntax of the function is:
//
//	CHAR(number)
func (fn *formulaFuncs) CHAR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHAR requires 1 argument")
	}
	arg := argsList.Front().Value.(formulaArg).ToNumber()
	if arg.Type != ArgNumber {
		return arg
	}
	num := int(arg.Number)
	if num < 0 || num > MaxFieldLength {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newStringFormulaArg(fmt.Sprintf("%c", num))
}

// CLEAN removes all non-printable characters from a supplied text string. The
// syntax of the function is:
//
//	CLEAN(text)
func (fn *formulaFuncs) CLEAN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "CLEAN requires 1 argument")
	}
	b := bytes.Buffer{}
	for _, c := range argsList.Front().Value.(formulaArg).Value() {
		if c > 31 {
			b.WriteRune(c)
		}
	}
	return newStringFormulaArg(b.String())
}

// CODE function converts the first character of a supplied text string into
// the associated numeric character set code used by your computer. The
// syntax of the function is:
//
//	CODE(text)
func (fn *formulaFuncs) CODE(argsList *list.List) formulaArg {
	return fn.code("CODE", argsList)
}

// code is an implementation of the formula functions CODE and UNICODE.
func (fn *formulaFuncs) code(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 1 argument", name))
	}
	text := argsList.Front().Value.(formulaArg).Value()
	if len(text) == 0 {
		if name == "CODE" {
			return newNumberFormulaArg(0)
		}
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newNumberFormulaArg(float64(text[0]))
}

// CONCAT function joins together a series of supplied text strings into one
// combined text string.
//
//	CONCAT(text1,[text2],...)
func (fn *formulaFuncs) CONCAT(argsList *list.List) formulaArg {
	return fn.concat("CONCAT", argsList)
}

// CONCATENATE function joins together a series of supplied text strings into
// one combined text string.
//
//	CONCATENATE(text1,[text2],...)
func (fn *formulaFuncs) CONCATENATE(argsList *list.List) formulaArg {
	return fn.concat("CONCATENATE", argsList)
}

// concat is an implementation of the formula functions CONCAT and
// CONCATENATE.
func (fn *formulaFuncs) concat(name string, argsList *list.List) formulaArg {
	var buf bytes.Buffer
	for arg := argsList.Front(); arg != nil; arg = arg.Next() {
		for _, cell := range arg.Value.(formulaArg).ToList() {
			if cell.Type == ArgError {
				return cell
			}
			buf.WriteString(cell.Value())
		}
	}
	return newStringFormulaArg(buf.String())
}

// EXACT function tests if two supplied text strings or values are exactly
// equal and if so, returns TRUE; Otherwise, the function returns FALSE. The
// function is case-sensitive. The syntax of the function is:
//
//	EXACT(text1,text2)
func (fn *formulaFuncs) EXACT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "EXACT requires 2 arguments")
	}
	text1 := argsList.Front().Value.(formulaArg).Value()
	text2 := argsList.Back().Value.(formulaArg).Value()
	return newBoolFormulaArg(text1 == text2)
}

// FIXED function rounds a supplied number to a specified number of decimal
// places and then converts this into text. The syntax of the function is:
//
//	FIXED(number,[decimals],[no_commas])
func (fn *formulaFuncs) FIXED(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FIXED requires at least 1 argument")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "FIXED allows at most 3 arguments")
	}
	numArg := argsList.Front().Value.(formulaArg).ToNumber()
	if numArg.Type != ArgNumber {
		return numArg
	}
	precision, decimals, noCommas := 0, 0, false
	s := strings.Split(argsList.Front().Value.(formulaArg).Value(), ".")
	if argsList.Len() == 1 && len(s) == 2 {
		precision = len(s[1])
		decimals = len(s[1])
	}
	if argsList.Len() >= 2 {
		decimalsArg := argsList.Front().Next().Value.(formulaArg).ToNumber()
		if decimalsArg.Type != ArgNumber {
			return decimalsArg
		}
		decimals = int(decimalsArg.Number)
	}
	if argsList.Len() == 3 {
		noCommasArg := argsList.Back().Value.(formulaArg).ToBool()
		if noCommasArg.Type == ArgError {
			return noCommasArg
		}
		noCommas = noCommasArg.Boolean
	}
	n := math.Pow(10, float64(decimals))
	r := numArg.Number * n
	fixed := float64(int(r+math.Copysign(0.5, r))) / n
	if decimals > 0 {
		precision = decimals
	}
	if noCommas {
		return newStringFormulaArg(fmt.Sprintf(fmt.Sprintf("%%.%df", precision), fixed))
	}
	p := message.NewPrinter(language.English)
	return newStringFormulaArg(p.Sprintf(fmt.Sprintf("%%.%df", precision), fixed))
}

// FIND function returns the position of a specified character or sub-string
// within a supplied text string. The function is case-sensitive. The syntax
// of the function is:
//
//	FIND(find_text,within_text,[start_num])
func (fn *formulaFuncs) FIND(argsList *list.List) formulaArg {
	return fn.find("FIND", argsList)
}

// FINDB counts each double-byte character as 2 when you have enabled the
// editing of a language that supports DBCS and then set it as the default
// language. Otherwise, FINDB counts each character as 1. The syntax of the
// function is:
//
//	FINDB(find_text,within_text,[start_num])
func (fn *formulaFuncs) FINDB(argsList *list.List) formulaArg {
	return fn.find("FINDB", argsList)
}

// prepareFindArgs checking and prepare arguments for the formula functions
// FIND, FINDB, SEARCH and SEARCHB.
func (fn *formulaFuncs) prepareFindArgs(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 2 arguments", name))
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 3 arguments", name))
	}
	startNum := 1
	if argsList.Len() == 3 {
		numArg := argsList.Back().Value.(formulaArg).ToNumber()
		if numArg.Type != ArgNumber {
			return numArg
		}
		if numArg.Number < 0 {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		startNum = int(numArg.Number)
	}
	return newListFormulaArg([]formulaArg{newNumberFormulaArg(float64(startNum))})
}

// find is an implementation of the formula functions FIND, FINDB, SEARCH and
// SEARCHB.
func (fn *formulaFuncs) find(name string, argsList *list.List) formulaArg {
	args := fn.prepareFindArgs(name, argsList)
	if args.Type != ArgList {
		return args
	}
	findText := argsList.Front().Value.(formulaArg).Value()
	withinText := argsList.Front().Next().Value.(formulaArg).Value()
	startNum := int(args.List[0].Number)
	if findText == "" {
		return newNumberFormulaArg(float64(startNum))
	}
	dbcs, search := name == "FINDB" || name == "SEARCHB", name == "SEARCH" || name == "SEARCHB"
	if search {
		findText, withinText = strings.ToUpper(findText), strings.ToUpper(withinText)
	}
	offset, ok := matchPattern(findText, withinText, dbcs, startNum)
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	result := offset
	if dbcs {
		var pre int
		for idx := range withinText {
			if pre > offset {
				break
			}
			if idx-pre > 1 {
				result++
			}
			pre = idx
		}
	}
	return newNumberFormulaArg(float64(result))
}

// LEFT function returns a specified number of characters from the start of a
// supplied text string. The syntax of the function is:
//
//	LEFT(text,[num_chars])
func (fn *formulaFuncs) LEFT(argsList *list.List) formulaArg {
	return fn.leftRight("LEFT", argsList)
}

// LEFTB returns the first character or characters in a text string, based on
// the number of bytes you specify. The syntax of the function is:
//
//	LEFTB(text,[num_bytes])
func (fn *formulaFuncs) LEFTB(argsList *list.List) formulaArg {
	return fn.leftRight("LEFTB", argsList)
}

// leftRight is an implementation of the formula functions LEFT, LEFTB, RIGHT,
// RIGHTB.
func (fn *formulaFuncs) leftRight(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 1 argument", name))
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 2 arguments", name))
	}
	text, numChars := argsList.Front().Value.(formulaArg).Value(), 1
	if argsList.Len() == 2 {
		numArg := argsList.Back().Value.(formulaArg).ToNumber()
		if numArg.Type != ArgNumber {
			return numArg
		}
		if numArg.Number < 0 {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		numChars = int(numArg.Number)
	}
	if name == "LEFTB" || name == "RIGHTB" {
		if len(text) > numChars {
			if name == "LEFTB" {
				return newStringFormulaArg(text[:numChars])
			}
			// RIGHTB
			return newStringFormulaArg(text[len(text)-numChars:])
		}
		return newStringFormulaArg(text)
	}
	// LEFT/RIGHT
	if utf8.RuneCountInString(text) > numChars {
		if name == "LEFT" {
			return newStringFormulaArg(string([]rune(text)[:numChars]))
		}
		// RIGHT
		return newStringFormulaArg(string([]rune(text)[utf8.RuneCountInString(text)-numChars:]))
	}
	return newStringFormulaArg(text)
}

// LEN returns the length of a supplied text string. The syntax of the
// function is:
//
//	LEN(text)
func (fn *formulaFuncs) LEN(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "LEN requires 1 string argument")
	}
	return newNumberFormulaArg(float64(utf8.RuneCountInString(argsList.Front().Value.(formulaArg).String)))
}

// LENB returns the number of bytes used to represent the characters in a text
// string. LENB counts 2 bytes per character only when a DBCS language is set
// as the default language. Otherwise LENB behaves the same as LEN, counting
// 1 byte per character. The syntax of the function is:
//
//	LENB(text)
func (fn *formulaFuncs) LENB(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "LENB requires 1 string argument")
	}
	bytes := 0
	for _, r := range argsList.Front().Value.(formulaArg).Value() {
		b := utf8.RuneLen(r)
		if b == 1 {
			bytes++
		} else if b > 1 {
			bytes += 2
		}
	}
	return newNumberFormulaArg(float64(bytes))
}

// LOWER converts all characters in a supplied text string to lower case. The
// syntax of the function is:
//
//	LOWER(text)
func (fn *formulaFuncs) LOWER(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "LOWER requires 1 argument")
	}
	return newStringFormulaArg(strings.ToLower(argsList.Front().Value.(formulaArg).String))
}

// MID function returns a specified number of characters from the middle of a
// supplied text string. The syntax of the function is:
//
//	MID(text,start_num,num_chars)
func (fn *formulaFuncs) MID(argsList *list.List) formulaArg {
	return fn.mid("MID", argsList)
}

// MIDB returns a specific number of characters from a text string, starting
// at the position you specify, based on the number of bytes you specify. The
// syntax of the function is:
//
//	MID(text,start_num,num_chars)
func (fn *formulaFuncs) MIDB(argsList *list.List) formulaArg {
	return fn.mid("MIDB", argsList)
}

// mid is an implementation of the formula functions MID and MIDB.
func (fn *formulaFuncs) mid(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 3 arguments", name))
	}
	text := argsList.Front().Value.(formulaArg).Value()
	startNumArg, numCharsArg := argsList.Front().Next().Value.(formulaArg).ToNumber(), argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if startNumArg.Type != ArgNumber {
		return startNumArg
	}
	if numCharsArg.Type != ArgNumber {
		return numCharsArg
	}
	startNum := int(startNumArg.Number)
	if startNum < 1 || numCharsArg.Number < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if name == "MIDB" {
		var result string
		var cnt, offset int
		for _, char := range text {
			offset++
			var dbcs bool
			if utf8.RuneLen(char) > 1 {
				dbcs = true
				offset++
			}
			if cnt == int(numCharsArg.Number) {
				break
			}
			if offset+1 > startNum {
				if dbcs {
					if cnt+2 > int(numCharsArg.Number) {
						result += string(char)[:1]
						break
					}
					result += string(char)
					cnt += 2
				} else {
					result += string(char)
					cnt++
				}
			}
		}
		return newStringFormulaArg(result)
	}
	// MID
	textLen := utf8.RuneCountInString(text)
	if startNum > textLen {
		return newStringFormulaArg("")
	}
	startNum--
	endNum := startNum + int(numCharsArg.Number)
	if endNum > textLen+1 {
		return newStringFormulaArg(string([]rune(text)[startNum:]))
	}
	return newStringFormulaArg(string([]rune(text)[startNum:endNum]))
}

// PROPER converts all characters in a supplied text string to proper case
// (i.e. all letters that do not immediately follow another letter are set to
// upper case and all other characters are lower case). The syntax of the
// function is:
//
//	PROPER(text)
func (fn *formulaFuncs) PROPER(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "PROPER requires 1 argument")
	}
	buf := bytes.Buffer{}
	isLetter := false
	for _, char := range argsList.Front().Value.(formulaArg).String {
		if !isLetter && unicode.IsLetter(char) {
			buf.WriteRune(unicode.ToUpper(char))
		} else {
			buf.WriteRune(unicode.ToLower(char))
		}
		isLetter = unicode.IsLetter(char)
	}
	return newStringFormulaArg(buf.String())
}

// REPLACE function replaces all or part of a text string with another string.
// The syntax of the function is:
//
//	REPLACE(old_text,start_num,num_chars,new_text)
func (fn *formulaFuncs) REPLACE(argsList *list.List) formulaArg {
	return fn.replace("REPLACE", argsList)
}

// REPLACEB replaces part of a text string, based on the number of bytes you
// specify, with a different text string.
//
//	REPLACEB(old_text,start_num,num_chars,new_text)
func (fn *formulaFuncs) REPLACEB(argsList *list.List) formulaArg {
	return fn.replace("REPLACEB", argsList)
}

// replace is an implementation of the formula functions REPLACE and REPLACEB.
// TODO: support DBCS include Japanese, Chinese (Simplified), Chinese
// (Traditional), and Korean.
func (fn *formulaFuncs) replace(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 4 arguments", name))
	}
	sourceText, targetText := argsList.Front().Value.(formulaArg).Value(), argsList.Back().Value.(formulaArg).Value()
	startNumArg, numCharsArg := argsList.Front().Next().Value.(formulaArg).ToNumber(), argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if startNumArg.Type != ArgNumber {
		return startNumArg
	}
	if numCharsArg.Type != ArgNumber {
		return numCharsArg
	}
	sourceTextLen, startIdx := len(sourceText), int(startNumArg.Number)
	if startIdx > sourceTextLen {
		startIdx = sourceTextLen + 1
	}
	endIdx := startIdx + int(numCharsArg.Number)
	if endIdx > sourceTextLen {
		endIdx = sourceTextLen + 1
	}
	if startIdx < 1 || endIdx < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	result := sourceText[:startIdx-1] + targetText + sourceText[endIdx-1:]
	return newStringFormulaArg(result)
}

// REPT function returns a supplied text string, repeated a specified number
// of times. The syntax of the function is:
//
//	REPT(text,number_times)
func (fn *formulaFuncs) REPT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "REPT requires 2 arguments")
	}
	text := argsList.Front().Value.(formulaArg)
	if text.Type != ArgString {
		return newErrorFormulaArg(formulaErrorVALUE, "REPT requires first argument to be a string")
	}
	times := argsList.Back().Value.(formulaArg).ToNumber()
	if times.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, "REPT requires second argument to be a number")
	}
	if times.Number < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "REPT requires second argument to be >= 0")
	}
	if times.Number == 0 {
		return newStringFormulaArg("")
	}
	buf := bytes.Buffer{}
	for i := 0; i < int(times.Number); i++ {
		buf.WriteString(text.String)
	}
	return newStringFormulaArg(buf.String())
}

// RIGHT function returns a specified number of characters from the end of a
// supplied text string. The syntax of the function is:
//
//	RIGHT(text,[num_chars])
func (fn *formulaFuncs) RIGHT(argsList *list.List) formulaArg {
	return fn.leftRight("RIGHT", argsList)
}

// RIGHTB returns the last character or characters in a text string, based on
// the number of bytes you specify. The syntax of the function is:
//
//	RIGHTB(text,[num_bytes])
func (fn *formulaFuncs) RIGHTB(argsList *list.List) formulaArg {
	return fn.leftRight("RIGHTB", argsList)
}

// SEARCH function returns the position of a specified character or sub-string
// within a supplied text string. The syntax of the function is:
//
//	SEARCH(search_text,within_text,[start_num])
func (fn *formulaFuncs) SEARCH(argsList *list.List) formulaArg {
	return fn.find("SEARCH", argsList)
}

// SEARCHB functions locate one text string within a second text string, and
// return the number of the starting position of the first text string from the
// first character of the second text string. The syntax of the function is:
//
//	SEARCHB(search_text,within_text,[start_num])
func (fn *formulaFuncs) SEARCHB(argsList *list.List) formulaArg {
	return fn.find("SEARCHB", argsList)
}

// SUBSTITUTE function replaces one or more instances of a given text string,
// within an original text string. The syntax of the function is:
//
//	SUBSTITUTE(text,old_text,new_text,[instance_num])
func (fn *formulaFuncs) SUBSTITUTE(argsList *list.List) formulaArg {
	if argsList.Len() != 3 && argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "SUBSTITUTE requires 3 or 4 arguments")
	}
	text, sourceText := argsList.Front().Value.(formulaArg), argsList.Front().Next().Value.(formulaArg)
	targetText, instanceNum := argsList.Front().Next().Next().Value.(formulaArg), 0
	if argsList.Len() == 3 {
		return newStringFormulaArg(strings.ReplaceAll(text.Value(), sourceText.Value(), targetText.Value()))
	}
	instanceNumArg := argsList.Back().Value.(formulaArg).ToNumber()
	if instanceNumArg.Type != ArgNumber {
		return instanceNumArg
	}
	instanceNum = int(instanceNumArg.Number)
	if instanceNum < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "instance_num should be > 0")
	}
	str, sourceTextLen, count, chars, pos := text.Value(), len(sourceText.Value()), instanceNum, 0, -1
	for {
		count--
		index := strings.Index(str, sourceText.Value())
		if index == -1 {
			pos = -1
			break
		} else {
			pos = index + chars
			if count == 0 {
				break
			}
			idx := sourceTextLen + index
			chars += idx
			str = str[idx:]
		}
	}
	if pos == -1 {
		return newStringFormulaArg(text.Value())
	}
	pre, post := text.Value()[:pos], text.Value()[pos+sourceTextLen:]
	return newStringFormulaArg(pre + targetText.Value() + post)
}

// TEXT function converts a supplied numeric value into text, in a
// user-specified format. The syntax of the function is:
//
//	TEXT(value,format_text)
func (fn *formulaFuncs) TEXT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "TEXT requires 2 arguments")
	}
	value, fmtText := argsList.Front().Value.(formulaArg), argsList.Back().Value.(formulaArg)
	if value.Type == ArgError {
		return value
	}
	if fmtText.Type == ArgError {
		return fmtText
	}
	cellType := CellTypeNumber
	if num := value.ToNumber(); num.Type != ArgNumber {
		cellType = CellTypeSharedString
	}
	return newStringFormulaArg(format(value.Value(), fmtText.Value(), false, cellType, nil))
}

// prepareTextAfterBefore checking and prepare arguments for the formula
// functions TEXTAFTER and TEXTBEFORE.
func (fn *formulaFuncs) prepareTextAfterBefore(name string, argsList *list.List) formulaArg {
	argsLen := argsList.Len()
	if argsLen < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 2 arguments", name))
	}
	if argsLen > 6 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s accepts at most 6 arguments", name))
	}
	text, delimiter := argsList.Front().Value.(formulaArg), argsList.Front().Next().Value.(formulaArg)
	instanceNum, matchMode, matchEnd, ifNotFound := newNumberFormulaArg(1), newBoolFormulaArg(false), newBoolFormulaArg(false), newEmptyFormulaArg()
	if argsLen > 2 {
		instanceNum = argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
		if instanceNum.Type != ArgNumber {
			return instanceNum
		}
	}
	if argsLen > 3 {
		matchMode = argsList.Front().Next().Next().Next().Value.(formulaArg).ToBool()
		if matchMode.Type != ArgNumber {
			return matchMode
		}
		if matchMode.Number == 1 {
			text, delimiter = newStringFormulaArg(strings.ToLower(text.Value())), newStringFormulaArg(strings.ToLower(delimiter.Value()))
		}
	}
	if argsLen > 4 {
		matchEnd = argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToBool()
		if matchEnd.Type != ArgNumber {
			return matchEnd
		}
	}
	if argsLen > 5 {
		ifNotFound = argsList.Back().Value.(formulaArg)
	}
	if text.Value() == "" {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	lenArgsList := list.New().Init()
	lenArgsList.PushBack(text)
	textLen := fn.LEN(lenArgsList)
	if instanceNum.Number == 0 || instanceNum.Number > textLen.Number {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	reverseSearch, startPos := instanceNum.Number < 0, 0.0
	if reverseSearch {
		startPos = textLen.Number
	}
	return newListFormulaArg([]formulaArg{
		text, delimiter, instanceNum, matchMode, matchEnd, ifNotFound,
		textLen, newBoolFormulaArg(reverseSearch), newNumberFormulaArg(startPos),
	})
}

// textAfterBeforeSearch is an implementation of the formula functions TEXTAFTER
// and TEXTBEFORE.
func textAfterBeforeSearch(text string, delimiter []string, startPos int, reverseSearch bool) (int, string) {
	idx := -1
	var modifiedDelimiter string
	for i := 0; i < len(delimiter); i++ {
		nextDelimiter := delimiter[i]
		nextIdx := strings.Index(text[startPos:], nextDelimiter)
		if nextIdx != -1 {
			nextIdx += startPos
		}
		if reverseSearch {
			nextIdx = strings.LastIndex(text[:startPos], nextDelimiter)
		}
		if idx == -1 || (((nextIdx < idx && !reverseSearch) || (nextIdx > idx && reverseSearch)) && idx != -1) {
			idx = nextIdx
			modifiedDelimiter = nextDelimiter
		}
	}
	return idx, modifiedDelimiter
}

// textAfterBeforeResult is an implementation of the formula functions TEXTAFTER
// and TEXTBEFORE.
func textAfterBeforeResult(name, modifiedDelimiter string, text []rune, foundIdx, repeatZero, textLen int, matchEndActive, matchEnd, reverseSearch bool) formulaArg {
	if name == "TEXTAFTER" {
		endPos := len(modifiedDelimiter)
		if (repeatZero > 1 || matchEndActive) && matchEnd && reverseSearch {
			endPos = 0
		}
		if foundIdx+endPos >= textLen {
			return newEmptyFormulaArg()
		}
		return newStringFormulaArg(string(text[foundIdx+endPos : textLen]))
	}
	return newStringFormulaArg(string(text[:foundIdx]))
}

// textAfterBefore is an implementation of the formula functions TEXTAFTER and
// TEXTBEFORE.
func (fn *formulaFuncs) textAfterBefore(name string, argsList *list.List) formulaArg {
	args := fn.prepareTextAfterBefore(name, argsList)
	if args.Type != ArgList {
		return args
	}
	var (
		text                 = []rune(argsList.Front().Value.(formulaArg).Value())
		modifiedText         = args.List[0].Value()
		delimiter            = []string{args.List[1].Value()}
		instanceNum          = args.List[2].Number
		matchEnd             = args.List[4].Number == 1
		ifNotFound           = args.List[5]
		textLen              = args.List[6]
		reverseSearch        = args.List[7].Number == 1
		foundIdx             = -1
		repeatZero, startPos int
		matchEndActive       bool
		modifiedDelimiter    string
	)
	if reverseSearch {
		startPos = int(args.List[8].Number)
	}
	for i := 0; i < int(math.Abs(instanceNum)); i++ {
		foundIdx, modifiedDelimiter = textAfterBeforeSearch(modifiedText, delimiter, startPos, reverseSearch)
		if foundIdx == 0 {
			repeatZero++
		}
		if foundIdx == -1 {
			if matchEnd && i == int(math.Abs(instanceNum))-1 {
				if foundIdx = int(textLen.Number); reverseSearch {
					foundIdx = 0
				}
				matchEndActive = true
			}
			break
		}
		if startPos = foundIdx + len(modifiedDelimiter); reverseSearch {
			startPos = foundIdx - len(modifiedDelimiter)
		}
	}
	if foundIdx == -1 {
		return ifNotFound
	}
	return textAfterBeforeResult(name, modifiedDelimiter, text, foundIdx, repeatZero, int(textLen.Number), matchEndActive, matchEnd, reverseSearch)
}

// TEXTAFTER function returns the text that occurs after a given substring or
// delimiter. The syntax of the function is:
//
//	TEXTAFTER(text,delimiter,[instance_num],[match_mode],[match_end],[if_not_found])
func (fn *formulaFuncs) TEXTAFTER(argsList *list.List) formulaArg {
	return fn.textAfterBefore("TEXTAFTER", argsList)
}

// TEXTBEFORE function returns text that occurs before a given character or
// string. The syntax of the function is:
//
//	TEXTBEFORE(text,delimiter,[instance_num],[match_mode],[match_end],[if_not_found])
func (fn *formulaFuncs) TEXTBEFORE(argsList *list.List) formulaArg {
	return fn.textAfterBefore("TEXTBEFORE", argsList)
}

// TEXTJOIN function joins together a series of supplied text strings into one
// combined text string. The user can specify a delimiter to add between the
// individual text items, if required. The syntax of the function is:
//
//	TEXTJOIN([delimiter],[ignore_empty],text1,[text2],...)
func (fn *formulaFuncs) TEXTJOIN(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TEXTJOIN requires at least 3 arguments")
	}
	if argsList.Len() > 252 {
		return newErrorFormulaArg(formulaErrorVALUE, "TEXTJOIN accepts at most 252 arguments")
	}
	delimiter := argsList.Front().Value.(formulaArg)
	ignoreEmpty := argsList.Front().Next().Value.(formulaArg)
	if ignoreEmpty.Type != ArgNumber || !ignoreEmpty.Boolean {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	args, ok := textJoin(argsList.Front().Next().Next(), []string{}, ignoreEmpty.Number != 0)
	if ok.Type != ArgNumber {
		return ok
	}
	result := strings.Join(args, delimiter.Value())
	if len(result) > TotalCellChars {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("TEXTJOIN function exceeds %d characters", TotalCellChars))
	}
	return newStringFormulaArg(result)
}

// textJoin is an implementation of the formula function TEXTJOIN.
func textJoin(arg *list.Element, arr []string, ignoreEmpty bool) ([]string, formulaArg) {
	for arg.Next(); arg != nil; arg = arg.Next() {
		switch arg.Value.(formulaArg).Type {
		case ArgError:
			return arr, arg.Value.(formulaArg)
		case ArgString, ArgEmpty:
			val := arg.Value.(formulaArg).Value()
			if val != "" || !ignoreEmpty {
				arr = append(arr, val)
			}
		case ArgNumber:
			arr = append(arr, arg.Value.(formulaArg).Value())
		case ArgMatrix:
			for _, row := range arg.Value.(formulaArg).Matrix {
				argList := list.New().Init()
				for _, ele := range row {
					argList.PushBack(ele)
				}
				if argList.Len() > 0 {
					args, _ := textJoin(argList.Front(), []string{}, ignoreEmpty)
					arr = append(arr, args...)
				}
			}
		}
	}
	return arr, newBoolFormulaArg(true)
}

// TRIM removes extra spaces (i.e. all spaces except for single spaces between
// words or characters) from a supplied text string. The syntax of the
// function is:
//
//	TRIM(text)
func (fn *formulaFuncs) TRIM(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TRIM requires 1 argument")
	}
	return newStringFormulaArg(strings.TrimSpace(argsList.Front().Value.(formulaArg).Value()))
}

// UNICHAR returns the Unicode character that is referenced by the given
// numeric value. The syntax of the function is:
//
//	UNICHAR(number)
func (fn *formulaFuncs) UNICHAR(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "UNICHAR requires 1 argument")
	}
	numArg := argsList.Front().Value.(formulaArg).ToNumber()
	if numArg.Type != ArgNumber {
		return numArg
	}
	if numArg.Number <= 0 || numArg.Number > 55295 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newStringFormulaArg(string(rune(numArg.Number)))
}

// UNICODE function returns the code point for the first character of a
// supplied text string. The syntax of the function is:
//
//	UNICODE(text)
func (fn *formulaFuncs) UNICODE(argsList *list.List) formulaArg {
	return fn.code("UNICODE", argsList)
}

// UPPER converts all characters in a supplied text string to upper case. The
// syntax of the function is:
//
//	UPPER(text)
func (fn *formulaFuncs) UPPER(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "UPPER requires 1 argument")
	}
	return newStringFormulaArg(strings.ToUpper(argsList.Front().Value.(formulaArg).String))
}

// VALUE function converts a text string into a numeric value. The syntax of
// the function is:
//
//	VALUE(text)
func (fn *formulaFuncs) VALUE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "VALUE requires 1 argument")
	}
	text := strings.ReplaceAll(argsList.Front().Value.(formulaArg).Value(), ",", "")
	percent := 1.0
	if strings.HasSuffix(text, "%") {
		percent, text = 0.01, strings.TrimSuffix(text, "%")
	}
	decimal := big.Float{}
	if _, ok := decimal.SetString(text); ok {
		value, _ := decimal.Float64()
		return newNumberFormulaArg(value * percent)
	}
	dateValue, timeValue, errTime, errDate := 0.0, 0.0, false, false
	if !isDateOnlyFmt(text) {
		h, m, s, _, _, err := strToTime(text)
		errTime = err.Type == ArgError
		if !errTime {
			timeValue = (float64(h)*3600 + float64(m)*60 + s) / 86400
		}
	}
	y, m, d, _, err := strToDate(text)
	errDate = err.Type == ArgError
	if !errDate {
		dateValue = daysBetween(excelMinTime1900.Unix(), makeDate(y, time.Month(m), d)) + 1
	}
	if errTime && errDate {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newNumberFormulaArg(dateValue + timeValue)
}

// VALUETOTEXT function returns text from any specified value. It passes text
// values unchanged, and converts non-text values to text.
//
//	VALUETOTEXT(value,[format])
func (fn *formulaFuncs) VALUETOTEXT(argsList *list.List) formulaArg {
	format := prepareToText("VALUETOTEXT", argsList)
	if format.Type != ArgNumber {
		return format
	}
	cell := argsList.Front().Value.(formulaArg)
	if num := cell.ToNumber(); num.Type != ArgNumber && format.Number == 1 {
		return newStringFormulaArg(fmt.Sprintf("\"%s\"", cell.Value()))
	}
	return newStringFormulaArg(cell.Value())
}

// Conditional Functions

// IF function tests a supplied condition and returns one result if the
// condition evaluates to TRUE, and another result if the condition evaluates
// to FALSE. The syntax of the function is:
//
//	IF(logical_test,value_if_true,value_if_false)
func (fn *formulaFuncs) IF(argsList *list.List) formulaArg {
	if argsList.Len() == 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "IF requires at least 1 argument")
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "IF accepts at most 3 arguments")
	}
	token := argsList.Front().Value.(formulaArg)
	var (
		cond   bool
		err    error
		result formulaArg
	)
	switch token.Type {
	case ArgString:
		if cond, err = strconv.ParseBool(token.String); err != nil {
			return newErrorFormulaArg(formulaErrorVALUE, err.Error())
		}
	case ArgNumber:
		cond = token.Number == 1
	}

	if argsList.Len() == 1 {
		return newBoolFormulaArg(cond)
	}
	if cond {
		value := argsList.Front().Next().Value.(formulaArg)
		switch value.Type {
		case ArgNumber:
			result = value.ToNumber()
		default:
			result = newStringFormulaArg(value.String)
		}
		return result
	}
	if argsList.Len() == 3 {
		value := argsList.Back().Value.(formulaArg)
		switch value.Type {
		case ArgNumber:
			result = value.ToNumber()
		default:
			result = newStringFormulaArg(value.String)
		}
	}
	return result
}

// Lookup and Reference Functions

// ADDRESS function takes a row and a column number and returns a cell
// reference as a text string. The syntax of the function is:
//
//	ADDRESS(row_num,column_num,[abs_num],[a1],[sheet_text])
func (fn *formulaFuncs) ADDRESS(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "ADDRESS requires at least 2 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "ADDRESS requires at most 5 arguments")
	}
	rowNum := argsList.Front().Value.(formulaArg).ToNumber()
	if rowNum.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if rowNum.Number >= TotalRows {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	colNum := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if colNum.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	absNum := newNumberFormulaArg(1)
	if argsList.Len() >= 3 {
		absNum = argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
		if absNum.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	if absNum.Number < 1 || absNum.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	a1 := newBoolFormulaArg(true)
	if argsList.Len() >= 4 {
		a1 = argsList.Front().Next().Next().Next().Value.(formulaArg).ToBool()
		if a1.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	var sheetText string
	if argsList.Len() == 5 {
		sheetText = fmt.Sprintf("%s!", argsList.Back().Value.(formulaArg).Value())
	}
	formatter := addressFmtMaps[fmt.Sprintf("%d_%s", int(absNum.Number), a1.Value())]
	addr, err := formatter(int(colNum.Number), int(rowNum.Number))
	if err != nil {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newStringFormulaArg(fmt.Sprintf("%s%s", sheetText, addr))
}

// CHOOSE function returns a value from an array, that corresponds to a
// supplied index number (position). The syntax of the function is:
//
//	CHOOSE(index_num,value1,[value2],...)
func (fn *formulaFuncs) CHOOSE(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "CHOOSE requires 2 arguments")
	}
	idx, err := strconv.Atoi(argsList.Front().Value.(formulaArg).Value())
	if err != nil {
		return newErrorFormulaArg(formulaErrorVALUE, "CHOOSE requires first argument of type number")
	}
	if argsList.Len() <= idx {
		return newErrorFormulaArg(formulaErrorVALUE, "index_num should be <= to the number of values")
	}
	arg := argsList.Front()
	for i := 0; i < idx; i++ {
		arg = arg.Next()
	}
	return arg.Value.(formulaArg)
}

// matchPatternToRegExp convert find text pattern to regular expression.
func matchPatternToRegExp(findText string, dbcs bool) (string, bool) {
	var (
		exp      string
		wildCard bool
		mark     = "."
	)
	if dbcs {
		mark = "(?:(?:[\\x00-\\x0081])|(?:[\\xFF61-\\xFFA0])|(?:[\\xF8F1-\\xF8F4])|[0-9A-Za-z])"
	}
	for _, char := range findText {
		if strings.ContainsAny(string(char), ".+$^[](){}|/") {
			exp += fmt.Sprintf("\\%s", string(char))
			continue
		}
		if char == '?' {
			wildCard = true
			exp += mark
			continue
		}
		if char == '*' {
			wildCard = true
			exp += ".*"
			continue
		}
		exp += string(char)
	}
	return fmt.Sprintf("^%s", exp), wildCard
}

// matchPattern finds whether the text matches or satisfies the pattern
// string. The pattern supports '*' and '?' wildcards in the pattern string.
func matchPattern(findText, withinText string, dbcs bool, startNum int) (int, bool) {
	exp, wildCard := matchPatternToRegExp(findText, dbcs)
	offset := 1
	for idx := range withinText {
		if offset < startNum {
			offset++
			continue
		}
		if wildCard {
			if ok, _ := regexp.MatchString(exp, withinText[idx:]); ok {
				break
			}
		}
		if strings.Index(withinText[idx:], findText) == 0 {
			break
		}
		offset++
	}
	return offset, utf8.RuneCountInString(withinText) != offset-1
}

// compareFormulaArg compares the left-hand sides and the right-hand sides'
// formula arguments by given conditions such as case-sensitive, if exact
// match, and make compare result as formula criteria condition type.
func compareFormulaArg(lhs, rhs, matchMode formulaArg, caseSensitive bool) byte {
	if lhs.Type != rhs.Type {
		return criteriaNe
	}
	switch lhs.Type {
	case ArgNumber:
		if lhs.Number == rhs.Number {
			return criteriaEq
		}
		if lhs.Number < rhs.Number {
			return criteriaL
		}
		return criteriaG
	case ArgString:
		ls, rs := lhs.String, rhs.String
		if !caseSensitive {
			ls, rs = strings.ToLower(ls), strings.ToLower(rs)
		}
		if matchMode.Number == matchModeWildcard {
			if _, ok := matchPattern(rs, ls, false, 0); ok {
				return criteriaEq
			}
		}
		return map[int]byte{1: criteriaG, -1: criteriaL, 0: criteriaEq}[strings.Compare(ls, rs)]
	case ArgEmpty:
		return criteriaEq
	case ArgList:
		return compareFormulaArgList(lhs, rhs, matchMode, caseSensitive)
	case ArgMatrix:
		return compareFormulaArgMatrix(lhs, rhs, matchMode, caseSensitive)
	default:
		return criteriaErr
	}
}

// compareFormulaArgList compares the left-hand sides and the right-hand sides
// list type formula arguments.
func compareFormulaArgList(lhs, rhs, matchMode formulaArg, caseSensitive bool) byte {
	if len(lhs.List) < len(rhs.List) {
		return criteriaL
	}
	if len(lhs.List) > len(rhs.List) {
		return criteriaG
	}
	for arg := range lhs.List {
		criteria := compareFormulaArg(lhs.List[arg], rhs.List[arg], matchMode, caseSensitive)
		if criteria != criteriaEq {
			return criteria
		}
	}
	return criteriaEq
}

// compareFormulaArgMatrix compares the left-hand sides and the right-hand sides'
// matrix type formula arguments.
func compareFormulaArgMatrix(lhs, rhs, matchMode formulaArg, caseSensitive bool) byte {
	if len(lhs.Matrix) < len(rhs.Matrix) {
		return criteriaL
	}
	if len(lhs.Matrix) > len(rhs.Matrix) {
		return criteriaG
	}
	for i := range lhs.Matrix {
		left, right := lhs.Matrix[i], rhs.Matrix[i]
		if len(left) < len(right) {
			return criteriaL
		}
		if len(left) > len(right) {
			return criteriaG
		}
		for arg := range left {
			criteria := compareFormulaArg(left[arg], right[arg], matchMode, caseSensitive)
			if criteria != criteriaEq {
				return criteria
			}
		}
	}
	return criteriaEq
}

// COLUMN function returns the first column number within a supplied reference
// or the number of the current column. The syntax of the function is:
//
//	COLUMN([reference])
func (fn *formulaFuncs) COLUMN(argsList *list.List) formulaArg {
	if argsList.Len() > 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COLUMN requires at most 1 argument")
	}
	if argsList.Len() == 1 {
		if argsList.Front().Value.(formulaArg).cellRanges != nil && argsList.Front().Value.(formulaArg).cellRanges.Len() > 0 {
			return newNumberFormulaArg(float64(argsList.Front().Value.(formulaArg).cellRanges.Front().Value.(cellRange).From.Col))
		}
		if argsList.Front().Value.(formulaArg).cellRefs != nil && argsList.Front().Value.(formulaArg).cellRefs.Len() > 0 {
			return newNumberFormulaArg(float64(argsList.Front().Value.(formulaArg).cellRefs.Front().Value.(cellRef).Col))
		}
		return newErrorFormulaArg(formulaErrorVALUE, "invalid reference")
	}
	col, _, _ := CellNameToCoordinates(fn.cell)
	return newNumberFormulaArg(float64(col))
}

// calcColsRowsMinMax calculation min and max value for given formula arguments
// sequence of the formula functions COLUMNS and ROWS.
func calcColsRowsMinMax(cols bool, argsList *list.List) (min, max int) {
	getVal := func(cols bool, cell cellRef) int {
		if cols {
			return cell.Col
		}
		return cell.Row
	}
	if argsList.Front().Value.(formulaArg).cellRanges != nil && argsList.Front().Value.(formulaArg).cellRanges.Len() > 0 {
		crs := argsList.Front().Value.(formulaArg).cellRanges
		for cr := crs.Front(); cr != nil; cr = cr.Next() {
			if min == 0 {
				min = getVal(cols, cr.Value.(cellRange).From)
			}
			if max < getVal(cols, cr.Value.(cellRange).To) {
				max = getVal(cols, cr.Value.(cellRange).To)
			}
		}
	}
	if argsList.Front().Value.(formulaArg).cellRefs != nil && argsList.Front().Value.(formulaArg).cellRefs.Len() > 0 {
		cr := argsList.Front().Value.(formulaArg).cellRefs
		for refs := cr.Front(); refs != nil; refs = refs.Next() {
			if min == 0 {
				min = getVal(cols, refs.Value.(cellRef))
			}
			if max < getVal(cols, refs.Value.(cellRef)) {
				max = getVal(cols, refs.Value.(cellRef))
			}
		}
	}
	return
}

// COLUMNS function receives an Excel range and returns the number of columns
// that are contained within the range. The syntax of the function is:
//
//	COLUMNS(array)
func (fn *formulaFuncs) COLUMNS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "COLUMNS requires 1 argument")
	}
	min, max := calcColsRowsMinMax(true, argsList)
	if max == MaxColumns {
		return newNumberFormulaArg(float64(MaxColumns))
	}
	result := max - min + 1
	if max == min {
		if min == 0 {
			return newErrorFormulaArg(formulaErrorVALUE, "invalid reference")
		}
		return newNumberFormulaArg(float64(1))
	}
	return newNumberFormulaArg(float64(result))
}

// FORMULATEXT function returns a formula as a text string. The syntax of the
// function is:
//
//	FORMULATEXT(reference)
func (fn *formulaFuncs) FORMULATEXT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "FORMULATEXT requires 1 argument")
	}
	refs := argsList.Front().Value.(formulaArg).cellRefs
	col, row := 0, 0
	if refs != nil && refs.Len() > 0 {
		col, row = refs.Front().Value.(cellRef).Col, refs.Front().Value.(cellRef).Row
	}
	ranges := argsList.Front().Value.(formulaArg).cellRanges
	if ranges != nil && ranges.Len() > 0 {
		col, row = ranges.Front().Value.(cellRange).From.Col, ranges.Front().Value.(cellRange).From.Row
	}
	cell, err := CoordinatesToCellName(col, row)
	if err != nil {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	formula, _ := fn.f.GetCellFormula(fn.sheet, cell)
	return newStringFormulaArg(formula)
}

// checkHVLookupArgs checking arguments, prepare extract mode, lookup value,
// and data for the formula functions HLOOKUP and VLOOKUP.
func checkHVLookupArgs(name string, argsList *list.List) (idx int, lookupValue, tableArray, matchMode, errArg formulaArg) {
	unit := map[string]string{
		"HLOOKUP": "row",
		"VLOOKUP": "col",
	}[name]
	if argsList.Len() < 3 {
		errArg = newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 3 arguments", name))
		return
	}
	if argsList.Len() > 4 {
		errArg = newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at most 4 arguments", name))
		return
	}
	lookupValue = argsList.Front().Value.(formulaArg)
	tableArray = argsList.Front().Next().Value.(formulaArg)
	if tableArray.Type != ArgMatrix {
		errArg = newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires second argument of table array", name))
		return
	}
	arg := argsList.Front().Next().Next().Value.(formulaArg)
	if arg.Type != ArgNumber || arg.Boolean {
		errArg = newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires numeric %s argument", name, unit))
		return
	}
	idx, matchMode = int(arg.Number)-1, newNumberFormulaArg(matchModeMaxLess)
	if argsList.Len() == 4 {
		rangeLookup := argsList.Back().Value.(formulaArg).ToBool()
		if rangeLookup.Type == ArgError {
			errArg = rangeLookup
			return
		}
		if rangeLookup.Number == 0 {
			matchMode = newNumberFormulaArg(matchModeWildcard)
		}
	}
	return
}

// HLOOKUP function 'looks up' a given value in the top row of a data array
// (or table), and returns the corresponding value from another row of the
// array. The syntax of the function is:
//
//	HLOOKUP(lookup_value,table_array,row_index_num,[range_lookup])
func (fn *formulaFuncs) HLOOKUP(argsList *list.List) formulaArg {
	rowIdx, lookupValue, tableArray, matchMode, errArg := checkHVLookupArgs("HLOOKUP", argsList)
	if errArg.Type == ArgError {
		return errArg
	}
	var matchIdx int
	var wasExact bool
	if matchMode.Number == matchModeWildcard || len(tableArray.Matrix) == TotalRows {
		matchIdx, wasExact = lookupLinearSearch(false, lookupValue, tableArray, matchMode, newNumberFormulaArg(searchModeLinear))
	} else {
		matchIdx, wasExact = lookupBinarySearch(false, lookupValue, tableArray, matchMode, newNumberFormulaArg(searchModeAscBinary))
	}
	if matchIdx == -1 {
		return newErrorFormulaArg(formulaErrorNA, "HLOOKUP no result found")
	}
	if rowIdx < 0 || rowIdx >= len(tableArray.Matrix) {
		return newErrorFormulaArg(formulaErrorNA, "HLOOKUP has invalid row index")
	}
	row := tableArray.Matrix[rowIdx]
	if wasExact || matchMode.Number == matchModeWildcard {
		return row[matchIdx]
	}
	return newErrorFormulaArg(formulaErrorNA, "HLOOKUP no result found")
}

// HYPERLINK function creates a hyperlink to a specified location. The syntax
// of the function is:
//
//	HYPERLINK(link_location,[friendly_name])
func (fn *formulaFuncs) HYPERLINK(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "HYPERLINK requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "HYPERLINK allows at most 2 arguments")
	}
	return newStringFormulaArg(argsList.Back().Value.(formulaArg).Value())
}

// calcMatch returns the position of the value by given match type, criteria
// and lookup array for the formula function MATCH.
func calcMatch(matchType int, criteria *formulaCriteria, lookupArray []formulaArg) formulaArg {
	idx := -1
	switch matchType {
	case 0:
		for i, arg := range lookupArray {
			if ok, _ := formulaCriteriaEval(arg, criteria); ok {
				return newNumberFormulaArg(float64(i + 1))
			}
		}
	case -1:
		for i, arg := range lookupArray {
			if ok, _ := formulaCriteriaEval(arg, &formulaCriteria{
				Type: criteriaGe, Condition: criteria.Condition,
			}); ok {
				idx = i
				continue
			}
			if criteria.Condition.Type == ArgNumber {
				break
			}
		}
	case 1:
		for i, arg := range lookupArray {
			if ok, _ := formulaCriteriaEval(arg, &formulaCriteria{
				Type: criteriaLe, Condition: criteria.Condition,
			}); ok {
				idx = i
				continue
			}
			if criteria.Condition.Type == ArgNumber {
				break
			}
		}
	}
	if idx == -1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	return newNumberFormulaArg(float64(idx + 1))
}

// MATCH function looks up a value in an array, and returns the position of
// the value within the array. The user can specify that the function should
// only return a result if an exact match is found, or that the function
// should return the position of the closest match (above or below), if an
// exact match is not found. The syntax of the Match function is:
//
//	MATCH(lookup_value,lookup_array,[match_type])
func (fn *formulaFuncs) MATCH(argsList *list.List) formulaArg {
	if argsList.Len() != 2 && argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "MATCH requires 1 or 2 arguments")
	}
	var (
		matchType      = 1
		lookupArray    []formulaArg
		lookupArrayArg = argsList.Front().Next().Value.(formulaArg)
		lookupArrayErr = "MATCH arguments lookup_array should be one-dimensional array"
	)
	if argsList.Len() == 3 {
		matchTypeArg := argsList.Back().Value.(formulaArg).ToNumber()
		if matchTypeArg.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, "MATCH requires numeric match_type argument")
		}
		if matchTypeArg.Number == -1 || matchTypeArg.Number == 0 {
			matchType = int(matchTypeArg.Number)
		}
	}
	switch lookupArrayArg.Type {
	case ArgMatrix:
		if len(lookupArrayArg.Matrix) != 1 && len(lookupArrayArg.Matrix[0]) != 1 {
			return newErrorFormulaArg(formulaErrorNA, lookupArrayErr)
		}
		lookupArray = lookupArrayArg.ToList()
	default:
		return newErrorFormulaArg(formulaErrorNA, lookupArrayErr)
	}
	return calcMatch(matchType, formulaCriteriaParser(argsList.Front().Value.(formulaArg)), lookupArray)
}

// TRANSPOSE function 'transposes' an array of cells (i.e. the function copies
// a horizontal range of cells into a vertical range and vice versa). The
// syntax of the function is:
//
//	TRANSPOSE(array)
func (fn *formulaFuncs) TRANSPOSE(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "TRANSPOSE requires 1 argument")
	}
	args := argsList.Back().Value.(formulaArg).ToList()
	rmin, rmax := calcColsRowsMinMax(false, argsList)
	cmin, cmax := calcColsRowsMinMax(true, argsList)
	cols, rows := cmax-cmin+1, rmax-rmin+1
	src := make([][]formulaArg, 0)
	for i := 0; i < len(args); i += cols {
		src = append(src, args[i:i+cols])
	}
	mtx := make([][]formulaArg, cols)
	for r, row := range src {
		colIdx := r % rows
		for c, cell := range row {
			rowIdx := c % cols
			if len(mtx[rowIdx]) == 0 {
				mtx[rowIdx] = make([]formulaArg, rows)
			}
			mtx[rowIdx][colIdx] = cell
		}
	}
	return newMatrixFormulaArg(mtx)
}

// lookupLinearSearch sequentially checks each look value of the lookup array until
// a match is found or the whole list has been searched.
func lookupLinearSearch(vertical bool, lookupValue, lookupArray, matchMode, searchMode formulaArg) (int, bool) {
	var tableArray []formulaArg
	if vertical {
		for _, row := range lookupArray.Matrix {
			tableArray = append(tableArray, row[0])
		}
	} else {
		tableArray = lookupArray.Matrix[0]
	}
	matchIdx, wasExact := -1, false
start:
	for i, cell := range tableArray {
		lhs := cell
		if lookupValue.Type == ArgNumber {
			if lhs = cell.ToNumber(); lhs.Type == ArgError {
				lhs = cell
			}
		} else if lookupValue.Type == ArgMatrix {
			lhs = lookupArray
		} else if lookupArray.Type == ArgString {
			lhs = newStringFormulaArg(cell.Value())
		}
		if compareFormulaArg(lhs, lookupValue, matchMode, false) == criteriaEq {
			matchIdx = i
			wasExact = true
			if searchMode.Number == searchModeLinear {
				break start
			}
		}
		if matchMode.Number == matchModeMinGreater || matchMode.Number == matchModeMaxLess {
			matchIdx = int(calcMatch(int(matchMode.Number), formulaCriteriaParser(lookupValue), tableArray).Number)
			continue
		}
	}
	return matchIdx, wasExact
}

// VLOOKUP function 'looks up' a given value in the left-hand column of a
// data array (or table), and returns the corresponding value from another
// column of the array. The syntax of the function is:
//
//	VLOOKUP(lookup_value,table_array,col_index_num,[range_lookup])
func (fn *formulaFuncs) VLOOKUP(argsList *list.List) formulaArg {
	colIdx, lookupValue, tableArray, matchMode, errArg := checkHVLookupArgs("VLOOKUP", argsList)
	if errArg.Type == ArgError {
		return errArg
	}
	var matchIdx int
	var wasExact bool
	if matchMode.Number == matchModeWildcard || len(tableArray.Matrix) == TotalRows {
		matchIdx, wasExact = lookupLinearSearch(true, lookupValue, tableArray, matchMode, newNumberFormulaArg(searchModeLinear))
	} else {
		matchIdx, wasExact = lookupBinarySearch(true, lookupValue, tableArray, matchMode, newNumberFormulaArg(searchModeAscBinary))
	}
	if matchIdx == -1 {
		return newErrorFormulaArg(formulaErrorNA, "VLOOKUP no result found")
	}
	mtx := tableArray.Matrix[matchIdx]
	if colIdx < 0 || colIdx >= len(mtx) {
		return newErrorFormulaArg(formulaErrorNA, "VLOOKUP has invalid column index")
	}
	if wasExact || matchMode.Number == matchModeWildcard {
		return mtx[colIdx]
	}
	return newErrorFormulaArg(formulaErrorNA, "VLOOKUP no result found")
}

// lookupBinarySearch finds the position of a target value when range lookup
// is TRUE, if the data of table array can't guarantee be sorted, it will
// return wrong result.
func lookupBinarySearch(vertical bool, lookupValue, lookupArray, matchMode, searchMode formulaArg) (matchIdx int, wasExact bool) {
	var tableArray []formulaArg
	if vertical {
		for _, row := range lookupArray.Matrix {
			tableArray = append(tableArray, row[0])
		}
	} else {
		tableArray = lookupArray.Matrix[0]
	}
	low, high, lastMatchIdx := 0, len(tableArray)-1, -1
	count := high
	for low <= high {
		mid := low + (high-low)/2
		cell := tableArray[mid]
		lhs := cell
		if lookupValue.Type == ArgNumber {
			if lhs = cell.ToNumber(); lhs.Type == ArgError {
				lhs = cell
			}
		} else if lookupValue.Type == ArgMatrix && vertical {
			lhs = lookupArray
		} else if lookupValue.Type == ArgString {
			lhs = newStringFormulaArg(cell.Value())
		}
		result := compareFormulaArg(lhs, lookupValue, matchMode, false)
		if result == criteriaEq {
			matchIdx, wasExact = mid, true
			if searchMode.Number == searchModeDescBinary {
				matchIdx = count - matchIdx
			}
			return
		} else if result == criteriaG {
			high = mid - 1
		} else if result == criteriaL {
			matchIdx = mid
			if cell.Type != ArgEmpty {
				lastMatchIdx = matchIdx
			}
			low = mid + 1
		} else {
			return -1, false
		}
	}
	matchIdx, wasExact = lastMatchIdx, true
	return
}

// checkLookupArgs checking arguments, prepare lookup value, and data for the
// formula function LOOKUP.
func checkLookupArgs(argsList *list.List) (arrayForm bool, lookupValue, lookupVector, errArg formulaArg) {
	if argsList.Len() < 2 {
		errArg = newErrorFormulaArg(formulaErrorVALUE, "LOOKUP requires at least 2 arguments")
		return
	}
	if argsList.Len() > 3 {
		errArg = newErrorFormulaArg(formulaErrorVALUE, "LOOKUP requires at most 3 arguments")
		return
	}
	lookupValue = newStringFormulaArg(argsList.Front().Value.(formulaArg).Value())
	lookupVector = argsList.Front().Next().Value.(formulaArg)
	if lookupVector.Type != ArgMatrix && lookupVector.Type != ArgList {
		errArg = newErrorFormulaArg(formulaErrorVALUE, "LOOKUP requires second argument of table array")
		return
	}
	arrayForm = lookupVector.Type == ArgMatrix
	if arrayForm && len(lookupVector.Matrix) == 0 {
		errArg = newErrorFormulaArg(formulaErrorVALUE, "LOOKUP requires not empty range as second argument")
	}
	return
}

// iterateLookupArgs iterate arguments to extract columns and calculate match
// index for the formula function LOOKUP.
func iterateLookupArgs(lookupValue, lookupVector formulaArg) ([]formulaArg, int, bool) {
	cols, matchIdx, ok := lookupCol(lookupVector, 0), -1, false
	for idx, col := range cols {
		lhs := lookupValue
		switch col.Type {
		case ArgNumber:
			lhs = lhs.ToNumber()
			if !col.Boolean {
				if lhs.Type == ArgError {
					lhs = lookupValue
				}
			}
		}
		compare := compareFormulaArg(lhs, col, newNumberFormulaArg(matchModeMaxLess), false)
		// Find exact match
		if compare == criteriaEq {
			matchIdx = idx
			break
		}
		// Find the nearest match if lookup value is more than or equal to the first value in lookup vector
		if idx == 0 {
			ok = compare == criteriaG
		} else if ok && compare == criteriaL && matchIdx == -1 {
			matchIdx = idx - 1
		}
	}
	return cols, matchIdx, ok
}

// index is an implementation of the formula function INDEX.
func (fn *formulaFuncs) index(array formulaArg, rowIdx, colIdx int) formulaArg {
	var cells []formulaArg
	if array.Type == ArgMatrix {
		cellMatrix := array.Matrix
		if rowIdx < -1 || rowIdx >= len(cellMatrix) {
			return newErrorFormulaArg(formulaErrorREF, "INDEX row_num out of range")
		}
		if rowIdx == -1 {
			if colIdx >= len(cellMatrix[0]) {
				return newErrorFormulaArg(formulaErrorREF, "INDEX col_num out of range")
			}
			var column [][]formulaArg
			for _, cells = range cellMatrix {
				column = append(column, []formulaArg{cells[colIdx]})
			}
			return newMatrixFormulaArg(column)
		}
		cells = cellMatrix[rowIdx]
	}
	if colIdx < -1 || colIdx >= len(cells) {
		return newErrorFormulaArg(formulaErrorREF, "INDEX col_num out of range")
	}
	return newListFormulaArg(cells)
}

// validateMatchMode check the number of match mode if be equal to 0, 1, -1 or
// 2.
func validateMatchMode(mode float64) bool {
	return mode == matchModeExact || mode == matchModeMinGreater || mode == matchModeMaxLess || mode == matchModeWildcard
}

// validateSearchMode check the number of search mode if be equal to 1, -1, 2
// or -2.
func validateSearchMode(mode float64) bool {
	return mode == searchModeLinear || mode == searchModeReverseLinear || mode == searchModeAscBinary || mode == searchModeDescBinary
}

// prepareXlookupArgs checking and prepare arguments for the formula function
// XLOOKUP.
func (fn *formulaFuncs) prepareXlookupArgs(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "XLOOKUP requires at least 3 arguments")
	}
	if argsList.Len() > 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "XLOOKUP allows at most 6 arguments")
	}
	lookupValue := argsList.Front().Value.(formulaArg)
	lookupArray := argsList.Front().Next().Value.(formulaArg)
	returnArray := argsList.Front().Next().Next().Value.(formulaArg)
	ifNotFond := newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	matchMode, searchMode := newNumberFormulaArg(matchModeExact), newNumberFormulaArg(searchModeLinear)
	if argsList.Len() > 3 {
		ifNotFond = argsList.Front().Next().Next().Next().Value.(formulaArg)
	}
	if argsList.Len() > 4 {
		if matchMode = argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); matchMode.Type != ArgNumber {
			return matchMode
		}
	}
	if argsList.Len() > 5 {
		if searchMode = argsList.Back().Value.(formulaArg).ToNumber(); searchMode.Type != ArgNumber {
			return searchMode
		}
	}
	if lookupArray.Type != ArgMatrix || returnArray.Type != ArgMatrix {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if !validateMatchMode(matchMode.Number) || !validateSearchMode(searchMode.Number) {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	return newListFormulaArg([]formulaArg{lookupValue, lookupArray, returnArray, ifNotFond, matchMode, searchMode})
}

// xlookup is an implementation of the formula function XLOOKUP.
func (fn *formulaFuncs) xlookup(lookupRows, lookupCols, returnArrayRows, returnArrayCols, matchIdx int,
	condition1, condition2, condition3, condition4 bool, returnArray formulaArg,
) formulaArg {
	var result [][]formulaArg
	for rowIdx, row := range returnArray.Matrix {
		for colIdx, cell := range row {
			if condition1 {
				if condition2 {
					result = append(result, []formulaArg{cell})
					continue
				}
				if returnArrayRows > 1 && returnArrayCols > 1 {
					return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
				}
			}
			if condition3 {
				if returnArrayCols != lookupCols {
					return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
				}
				if colIdx == matchIdx {
					result = append(result, []formulaArg{cell})
					continue
				}
			}
			if condition4 {
				if returnArrayRows != lookupRows {
					return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
				}
				if rowIdx == matchIdx {
					if len(result) == 0 {
						result = append(result, []formulaArg{cell})
						continue
					}
					result[0] = append(result[0], cell)
				}
			}
		}
	}
	array := newMatrixFormulaArg(result)
	cells := array.ToList()
	if len(cells) == 1 {
		return cells[0]
	}
	return array
}

// XLOOKUP function searches a range or an array, and then returns the item
// corresponding to the first match it finds. If no match exists, then
// XLOOKUP can return the closest (approximate) match. The syntax of the
// function is:
//
//	XLOOKUP(lookup_value,lookup_array,return_array,[if_not_found],[match_mode],[search_mode])
func (fn *formulaFuncs) XLOOKUP(argsList *list.List) formulaArg {
	args := fn.prepareXlookupArgs(argsList)
	if args.Type != ArgList {
		return args
	}
	lookupValue, lookupArray, returnArray, ifNotFond, matchMode, searchMode := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5]
	lookupRows, lookupCols := len(lookupArray.Matrix), 0
	if lookupRows > 0 {
		lookupCols = len(lookupArray.Matrix[0])
	}
	if lookupRows != 1 && lookupCols != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	verticalLookup := lookupRows >= lookupCols
	var matchIdx int
	switch searchMode.Number {
	case searchModeLinear, searchModeReverseLinear:
		matchIdx, _ = lookupLinearSearch(verticalLookup, lookupValue, lookupArray, matchMode, searchMode)
	default:
		matchIdx, _ = lookupBinarySearch(verticalLookup, lookupValue, lookupArray, matchMode, searchMode)
	}
	if matchIdx == -1 {
		return ifNotFond
	}
	returnArrayRows, returnArrayCols := len(returnArray.Matrix), len(returnArray.Matrix[0])
	condition1 := lookupRows == 1 && lookupCols == 1
	condition2 := returnArrayRows == 1 || returnArrayCols == 1
	condition3 := lookupRows == 1 && lookupCols > 1
	condition4 := lookupRows > 1 && lookupCols == 1
	return fn.xlookup(lookupRows, lookupCols, returnArrayRows, returnArrayCols, matchIdx, condition1, condition2, condition3, condition4, returnArray)
}

// INDEX function returns a reference to a cell that lies in a specified row
// and column of a range of cells. The syntax of the function is:
//
//	INDEX(array,row_num,[col_num])
func (fn *formulaFuncs) INDEX(argsList *list.List) formulaArg {
	if argsList.Len() < 2 || argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "INDEX requires 2 or 3 arguments")
	}
	array := argsList.Front().Value.(formulaArg)
	if array.Type != ArgMatrix && array.Type != ArgList {
		array = newMatrixFormulaArg([][]formulaArg{{array}})
	}
	rowArg := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if rowArg.Type != ArgNumber {
		return rowArg
	}
	rowIdx, colIdx := int(rowArg.Number)-1, -1
	if argsList.Len() == 3 {
		colArg := argsList.Back().Value.(formulaArg).ToNumber()
		if colArg.Type != ArgNumber {
			return colArg
		}
		colIdx = int(colArg.Number) - 1
	}
	if rowIdx == -1 && colIdx == -1 {
		if len(array.ToList()) != 1 {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		return array.ToList()[0]
	}
	cells := fn.index(array, rowIdx, colIdx)
	if cells.Type != ArgList {
		return cells
	}
	if colIdx == -1 {
		return newMatrixFormulaArg([][]formulaArg{cells.List})
	}
	return cells.List[colIdx]
}

// INDIRECT function converts a text string into a cell reference. The syntax
// of the Indirect function is:
//
//	INDIRECT(ref_text,[a1])
func (fn *formulaFuncs) INDIRECT(argsList *list.List) formulaArg {
	if argsList.Len() != 1 && argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "INDIRECT requires 1 or 2 arguments")
	}
	refText := argsList.Front().Value.(formulaArg).Value()
	a1 := newBoolFormulaArg(true)
	if argsList.Len() == 2 {
		if a1 = argsList.Back().Value.(formulaArg).ToBool(); a1.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	R1C1ToA1 := func(ref string) (cell string, err error) {
		parts := strings.Split(strings.TrimLeft(ref, "R"), "C")
		if len(parts) != 2 {
			return
		}
		row, err := strconv.Atoi(parts[0])
		if err != nil {
			return
		}
		col, err := strconv.Atoi(parts[1])
		if err != nil {
			return
		}
		cell, err = CoordinatesToCellName(col, row)
		return
	}
	refs := strings.Split(refText, ":")
	fromRef, toRef := refs[0], ""
	if len(refs) == 2 {
		toRef = refs[1]
	}
	if a1.Number == 0 {
		from, err := R1C1ToA1(refs[0])
		if err != nil {
			return newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
		}
		fromRef = from
		if len(refs) == 2 {
			to, err := R1C1ToA1(refs[1])
			if err != nil {
				return newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
			}
			toRef = to
		}
	}
	if len(refs) == 1 {
		value, err := fn.f.GetCellValue(fn.sheet, fromRef)
		if err != nil {
			return newErrorFormulaArg(formulaErrorREF, formulaErrorREF)
		}
		return newStringFormulaArg(value)
	}
	arg, _ := fn.f.parseReference(fn.ctx, fn.sheet, fromRef+":"+toRef)
	return arg
}

// LOOKUP function performs an approximate match lookup in a one-column or
// one-row range, and returns the corresponding value from another one-column
// or one-row range. The syntax of the function is:
//
//	LOOKUP(lookup_value,lookup_vector,[result_vector])
func (fn *formulaFuncs) LOOKUP(argsList *list.List) formulaArg {
	arrayForm, lookupValue, lookupVector, errArg := checkLookupArgs(argsList)
	if errArg.Type == ArgError {
		return errArg
	}
	cols, matchIdx, ok := iterateLookupArgs(lookupValue, lookupVector)
	if ok && matchIdx == -1 {
		matchIdx = len(cols) - 1
	}
	var column []formulaArg
	if argsList.Len() == 3 {
		column = lookupCol(argsList.Back().Value.(formulaArg), 0)
	} else if arrayForm && len(lookupVector.Matrix[0]) > 1 {
		column = lookupCol(lookupVector, 1)
	} else {
		column = cols
	}
	if matchIdx < 0 || matchIdx >= len(column) {
		return newErrorFormulaArg(formulaErrorNA, "LOOKUP no result found")
	}
	return column[matchIdx]
}

// lookupCol extract columns for LOOKUP.
func lookupCol(arr formulaArg, idx int) []formulaArg {
	col := arr.List
	if arr.Type == ArgMatrix {
		col = nil
		for _, r := range arr.Matrix {
			if len(r) > 0 {
				col = append(col, r[idx])
				continue
			}
			col = append(col, newEmptyFormulaArg())
		}
	}
	return col
}

// ROW function returns the first row number within a supplied reference or
// the number of the current row. The syntax of the function is:
//
//	ROW([reference])
func (fn *formulaFuncs) ROW(argsList *list.List) formulaArg {
	if argsList.Len() > 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROW requires at most 1 argument")
	}
	if argsList.Len() == 1 {
		if argsList.Front().Value.(formulaArg).cellRanges != nil && argsList.Front().Value.(formulaArg).cellRanges.Len() > 0 {
			return newNumberFormulaArg(float64(argsList.Front().Value.(formulaArg).cellRanges.Front().Value.(cellRange).From.Row))
		}
		if argsList.Front().Value.(formulaArg).cellRefs != nil && argsList.Front().Value.(formulaArg).cellRefs.Len() > 0 {
			return newNumberFormulaArg(float64(argsList.Front().Value.(formulaArg).cellRefs.Front().Value.(cellRef).Row))
		}
		return newErrorFormulaArg(formulaErrorVALUE, "invalid reference")
	}
	_, row, _ := CellNameToCoordinates(fn.cell)
	return newNumberFormulaArg(float64(row))
}

// ROWS function takes an Excel range and returns the number of rows that are
// contained within the range. The syntax of the function is:
//
//	ROWS(array)
func (fn *formulaFuncs) ROWS(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ROWS requires 1 argument")
	}
	min, max := calcColsRowsMinMax(false, argsList)
	if max == TotalRows {
		return newNumberFormulaArg(TotalRows)
	}
	result := max - min + 1
	if max == min {
		if min == 0 {
			return newErrorFormulaArg(formulaErrorVALUE, "invalid reference")
		}
		return newNumberFormulaArg(float64(1))
	}
	return newNumberFormulaArg(float64(result))
}

// Web Functions

// ENCODEURL function returns a URL-encoded string, replacing certain
// non-alphanumeric characters with the percentage symbol (%) and a
// hexadecimal number. The syntax of the function is:
//
//	ENCODEURL(url)
func (fn *formulaFuncs) ENCODEURL(argsList *list.List) formulaArg {
	if argsList.Len() != 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "ENCODEURL requires 1 argument")
	}
	token := argsList.Front().Value.(formulaArg).Value()
	return newStringFormulaArg(strings.ReplaceAll(url.QueryEscape(token), "+", "%20"))
}

// Financial Functions

// validateFrequency check the number of coupon payments per year if be equal to 1, 2 or 4.
func validateFrequency(freq float64) bool {
	return freq == 1 || freq == 2 || freq == 4
}

// ACCRINT function returns the accrued interest in a security that pays
// periodic interest. The syntax of the function is:
//
//	ACCRINT(issue,first_interest,settlement,rate,par,frequency,[basis],[calc_method])
func (fn *formulaFuncs) ACCRINT(argsList *list.List) formulaArg {
	if argsList.Len() < 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACCRINT requires at least 6 arguments")
	}
	if argsList.Len() > 8 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACCRINT allows at most 8 arguments")
	}
	args := fn.prepareDataValueArgs(3, argsList)
	if args.Type != ArgList {
		return args
	}
	issue, settlement := args.List[0], args.List[2]
	rate := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	par := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	frequency := argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber || par.Type != ArgNumber || frequency.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() >= 7 {
		if basis = argsList.Front().Next().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	if argsList.Len() == 8 {
		if cm := argsList.Back().Value.(formulaArg).ToBool(); cm.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	frac1 := yearFrac(issue.Number, settlement.Number, int(basis.Number))
	if frac1.Type != ArgNumber {
		return frac1
	}
	return newNumberFormulaArg(par.Number * rate.Number * frac1.Number)
}

// ACCRINTM function returns the accrued interest in a security that pays
// interest at maturity. The syntax of the function is:
//
//	ACCRINTM(issue,settlement,rate,[par],[basis])
func (fn *formulaFuncs) ACCRINTM(argsList *list.List) formulaArg {
	if argsList.Len() != 4 && argsList.Len() != 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "ACCRINTM requires 4 or 5 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	issue, settlement := args.List[0], args.List[1]
	if settlement.Number < issue.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	rate := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	par := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber || par.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if par.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	frac := yearFrac(issue.Number, settlement.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	return newNumberFormulaArg(frac.Number * rate.Number * par.Number)
}

// prepareAmorArgs checking and prepare arguments for the formula functions
// AMORDEGRC and AMORLINC.
func (fn *formulaFuncs) prepareAmorArgs(name string, argsList *list.List) formulaArg {
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires cost to be number argument", name))
	}
	if cost.Number < 0 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires cost >= 0", name))
	}
	args := list.New().Init()
	args.PushBack(argsList.Front().Next().Value.(formulaArg))
	datePurchased := fn.DATEVALUE(args)
	if datePurchased.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	args.Init()
	args.PushBack(argsList.Front().Next().Next().Value.(formulaArg))
	firstPeriod := fn.DATEVALUE(args)
	if firstPeriod.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if firstPeriod.Number < datePurchased.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	salvage := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if salvage.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if salvage.Number < 0 || salvage.Number > cost.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	period := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if period.Type != ArgNumber || period.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	rate := argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber || rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 7 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newListFormulaArg([]formulaArg{cost, datePurchased, firstPeriod, salvage, period, rate, basis})
}

// AMORDEGRC function is provided for users of the French accounting system.
// The function calculates the prorated linear depreciation of an asset for a
// specified accounting period. The syntax of the function is:
//
//	AMORDEGRC(cost,date_purchased,first_period,salvage,period,rate,[basis])
func (fn *formulaFuncs) AMORDEGRC(argsList *list.List) formulaArg {
	if argsList.Len() != 6 && argsList.Len() != 7 {
		return newErrorFormulaArg(formulaErrorVALUE, "AMORDEGRC requires 6 or 7 arguments")
	}
	args := fn.prepareAmorArgs("AMORDEGRC", argsList)
	if args.Type != ArgList {
		return args
	}
	cost, datePurchased, firstPeriod, salvage, period, rate, basis := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5], args.List[6]
	if rate.Number >= 0.5 {
		return newErrorFormulaArg(formulaErrorNUM, "AMORDEGRC requires rate to be < 0.5")
	}
	assetsLife, amorCoeff := 1/rate.Number, 2.5
	if assetsLife < 3 {
		amorCoeff = 1
	} else if assetsLife < 5 {
		amorCoeff = 1.5
	} else if assetsLife <= 6 {
		amorCoeff = 2
	}
	rate.Number *= amorCoeff
	frac := yearFrac(datePurchased.Number, firstPeriod.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	nRate := float64(int((frac.Number * cost.Number * rate.Number) + 0.5))
	cost.Number -= nRate
	rest := cost.Number - salvage.Number
	for n := 0; n < int(period.Number); n++ {
		nRate = float64(int((cost.Number * rate.Number) + 0.5))
		rest -= nRate
		if rest < 0 {
			switch int(period.Number) - n {
			case 0:
			case 1:
				return newNumberFormulaArg(float64(int((cost.Number * 0.5) + 0.5)))
			default:
				return newNumberFormulaArg(0)
			}
		}
		cost.Number -= nRate
	}
	return newNumberFormulaArg(nRate)
}

// AMORLINC function is provided for users of the French accounting system.
// The function calculates the prorated linear depreciation of an asset for a
// specified accounting period. The syntax of the function is:
//
//	AMORLINC(cost,date_purchased,first_period,salvage,period,rate,[basis])
func (fn *formulaFuncs) AMORLINC(argsList *list.List) formulaArg {
	if argsList.Len() != 6 && argsList.Len() != 7 {
		return newErrorFormulaArg(formulaErrorVALUE, "AMORLINC requires 6 or 7 arguments")
	}
	args := fn.prepareAmorArgs("AMORLINC", argsList)
	if args.Type != ArgList {
		return args
	}
	cost, datePurchased, firstPeriod, salvage, period, rate, basis := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5], args.List[6]
	frac := yearFrac(datePurchased.Number, firstPeriod.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	rate1 := frac.Number * cost.Number * rate.Number
	if period.Number == 0 {
		return newNumberFormulaArg(rate1)
	}
	rate2 := cost.Number * rate.Number
	delta := cost.Number - salvage.Number
	periods := int((delta - rate1) / rate2)
	if int(period.Number) <= periods {
		return newNumberFormulaArg(rate2)
	} else if int(period.Number)-1 == periods {
		return newNumberFormulaArg(delta - rate2*float64(periods) - math.Nextafter(rate1, rate1))
	}
	return newNumberFormulaArg(0)
}

// prepareCouponArgs checking and prepare arguments for the formula functions
// COUPDAYBS, COUPDAYS, COUPDAYSNC, COUPPCD, COUPNUM and COUPNCD.
func (fn *formulaFuncs) prepareCouponArgs(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 3 && argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 3 or 4 arguments", name))
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	if settlement.Number >= maturity.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires maturity > settlement", name))
	}
	frequency := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if frequency.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 4 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newListFormulaArg([]formulaArg{settlement, maturity, frequency, basis})
}

// is30BasisMethod determine if the financial day count basis rules is 30/360
// methods.
func is30BasisMethod(basis int) bool {
	return basis == 0 || basis == 4
}

// getDaysInMonthRange return the day by given year, month range and day count
// basis.
func getDaysInMonthRange(fromMonth, toMonth int) int {
	if fromMonth > toMonth {
		return 0
	}
	return (toMonth - fromMonth + 1) * 30
}

// getDayOnBasis returns the day by given date and day count basis.
func getDayOnBasis(y, m, d, basis int) int {
	if !is30BasisMethod(basis) {
		return d
	}
	day := d
	dim := getDaysInMonth(y, m)
	if day > 30 || d >= dim || day >= dim {
		day = 30
	}
	return day
}

// coupdays returns the number of days that base on date range and the day
// count basis to be used.
func coupdays(from, to time.Time, basis int) float64 {
	days := 0
	fromY, fromM, fromD := from.Date()
	toY, toM, toD := to.Date()
	fromDay, toDay := getDayOnBasis(fromY, int(fromM), fromD, basis), getDayOnBasis(toY, int(toM), toD, basis)
	if !is30BasisMethod(basis) {
		return (daysBetween(excelMinTime1900.Unix(), makeDate(toY, toM, toDay)) + 1) - (daysBetween(excelMinTime1900.Unix(), makeDate(fromY, fromM, fromDay)) + 1)
	}
	if basis == 0 {
		if (int(fromM) == 2 || fromDay < 30) && toD == 31 {
			toDay = 31
		}
	} else {
		if int(fromM) == 2 && fromDay == 30 {
			fromDay = getDaysInMonth(fromY, 2)
		}
		if int(toM) == 2 && toDay == 30 {
			toDay = getDaysInMonth(toY, 2)
		}
	}
	if fromY < toY || (fromY == toY && int(fromM) < int(toM)) {
		days = 30 - fromDay + 1
		fromD = 1
		fromDay = 1
		date := time.Date(fromY, fromM, fromD, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		if date.Year() < toY {
			days += getDaysInMonthRange(int(date.Month()), 12)
			date = date.AddDate(0, 13-int(date.Month()), 0)
		}
		days += getDaysInMonthRange(int(date.Month()), int(toM)-1)
	}
	if days += toDay - fromDay; days > 0 {
		return float64(days)
	}
	return 0
}

// COUPDAYBS function calculates the number of days from the beginning of a
// coupon's period to the settlement date. The syntax of the function is:
//
//	COUPDAYBS(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPDAYBS(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPDAYBS", argsList)
	if args.Type != ArgList {
		return args
	}
	settlement := timeFromExcelTime(args.List[0].Number, false)
	pcd := timeFromExcelTime(fn.COUPPCD(argsList).Number, false)
	return newNumberFormulaArg(coupdays(pcd, settlement, int(args.List[3].Number)))
}

// COUPDAYS function calculates the number of days in a coupon period that
// contains the settlement date. The syntax of the function is:
//
//	COUPDAYS(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPDAYS(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPDAYS", argsList)
	if args.Type != ArgList {
		return args
	}
	freq := args.List[2].Number
	basis := int(args.List[3].Number)
	if basis == 1 {
		pcd := timeFromExcelTime(fn.COUPPCD(argsList).Number, false)
		next := pcd.AddDate(0, 12/int(freq), 0)
		return newNumberFormulaArg(coupdays(pcd, next, basis))
	}
	return newNumberFormulaArg(float64(getYearDays(0, basis)) / freq)
}

// COUPDAYSNC function calculates the number of days from the settlement date
// to the next coupon date. The syntax of the function is:
//
//	COUPDAYSNC(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPDAYSNC(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPDAYSNC", argsList)
	if args.Type != ArgList {
		return args
	}
	settlement := timeFromExcelTime(args.List[0].Number, false)
	basis := int(args.List[3].Number)
	ncd := timeFromExcelTime(fn.COUPNCD(argsList).Number, false)
	return newNumberFormulaArg(coupdays(settlement, ncd, basis))
}

// coupons is an implementation of the formula functions COUPNCD and COUPPCD.
func (fn *formulaFuncs) coupons(name string, arg formulaArg) formulaArg {
	settlement := timeFromExcelTime(arg.List[0].Number, false)
	maturity := timeFromExcelTime(arg.List[1].Number, false)
	maturityDays := (maturity.Year()-settlement.Year())*12 + (int(maturity.Month()) - int(settlement.Month()))
	coupon := 12 / int(arg.List[2].Number)
	mod := maturityDays % coupon
	year := settlement.Year()
	month := int(settlement.Month())
	if mod == 0 && settlement.Day() >= maturity.Day() {
		month += coupon
	} else {
		month += mod
	}
	if name != "COUPNCD" {
		month -= coupon
	}
	if month > 11 {
		year++
		month -= 12
	} else if month < 0 {
		year--
		month += 12
	}
	day, lastDay := maturity.Day(), time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	days := getDaysInMonth(lastDay.Year(), int(lastDay.Month()))
	if getDaysInMonth(maturity.Year(), int(maturity.Month())) == maturity.Day() {
		day = days
	} else if day > 27 && day > days {
		day = days
	}
	return newNumberFormulaArg(daysBetween(excelMinTime1900.Unix(), makeDate(year, time.Month(month), day)) + 1)
}

// COUPNCD function calculates the number of coupons payable, between a
// security's settlement date and maturity date, rounded up to the nearest
// whole coupon. The syntax of the function is:
//
//	COUPNCD(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPNCD(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPNCD", argsList)
	if args.Type != ArgList {
		return args
	}
	return fn.coupons("COUPNCD", args)
}

// COUPNUM function calculates the number of coupons payable, between a
// security's settlement date and maturity date, rounded up to the nearest
// whole coupon. The syntax of the function is:
//
//	COUPNUM(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPNUM(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPNUM", argsList)
	if args.Type != ArgList {
		return args
	}
	frac := yearFrac(args.List[0].Number, args.List[1].Number, 0)
	return newNumberFormulaArg(math.Ceil(frac.Number * args.List[2].Number))
}

// COUPPCD function returns the previous coupon date, before the settlement
// date for a security. The syntax of the function is:
//
//	COUPPCD(settlement,maturity,frequency,[basis])
func (fn *formulaFuncs) COUPPCD(argsList *list.List) formulaArg {
	args := fn.prepareCouponArgs("COUPPCD", argsList)
	if args.Type != ArgList {
		return args
	}
	return fn.coupons("COUPPCD", args)
}

// CUMIPMT function calculates the cumulative interest paid on a loan or
// investment, between two specified periods. The syntax of the function is:
//
//	CUMIPMT(rate,nper,pv,start_period,end_period,type)
func (fn *formulaFuncs) CUMIPMT(argsList *list.List) formulaArg {
	return fn.cumip("CUMIPMT", argsList)
}

// CUMPRINC function calculates the cumulative payment on the principal of a
// loan or investment, between two specified periods. The syntax of the
// function is:
//
//	CUMPRINC(rate,nper,pv,start_period,end_period,type)
func (fn *formulaFuncs) CUMPRINC(argsList *list.List) formulaArg {
	return fn.cumip("CUMPRINC", argsList)
}

// cumip is an implementation of the formula functions CUMIPMT and CUMPRINC.
func (fn *formulaFuncs) cumip(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 6 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 6 arguments", name))
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	nper := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pv := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	start := argsList.Back().Prev().Prev().Value.(formulaArg).ToNumber()
	if start.Type != ArgNumber {
		return start
	}
	end := argsList.Back().Prev().Value.(formulaArg).ToNumber()
	if end.Type != ArgNumber {
		return end
	}
	typ := argsList.Back().Value.(formulaArg).ToNumber()
	if typ.Type != ArgNumber {
		return typ
	}
	if typ.Number != 0 && typ.Number != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if start.Number < 1 || start.Number > end.Number {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	num := 0.0
	for per := start.Number; per <= end.Number; per++ {
		args := list.New().Init()
		args.PushBack(rate)
		args.PushBack(newNumberFormulaArg(per))
		args.PushBack(nper)
		args.PushBack(pv)
		args.PushBack(newNumberFormulaArg(0))
		args.PushBack(typ)
		if name == "CUMIPMT" {
			num += fn.IPMT(args).Number
			continue
		}
		num += fn.PPMT(args).Number
	}
	return newNumberFormulaArg(num)
}

// calcDbArgsCompare implements common arguments' comparison for DB and DDB.
func calcDbArgsCompare(cost, salvage, life, period formulaArg) bool {
	return (cost.Number <= 0) || ((salvage.Number / cost.Number) < 0) || (life.Number <= 0) || (period.Number < 1)
}

// DB function calculates the depreciation of an asset, using the Fixed
// Declining Balance Method, for each period of the asset's lifetime. The
// syntax of the function is:
//
//	DB(cost,salvage,life,period,[month])
func (fn *formulaFuncs) DB(argsList *list.List) formulaArg {
	if argsList.Len() < 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "DB requires at least 4 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "DB allows at most 5 arguments")
	}
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber {
		return cost
	}
	salvage := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if salvage.Type != ArgNumber {
		return salvage
	}
	life := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if life.Type != ArgNumber {
		return life
	}
	period := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if period.Type != ArgNumber {
		return period
	}
	month := newNumberFormulaArg(12)
	if argsList.Len() == 5 {
		if month = argsList.Back().Value.(formulaArg).ToNumber(); month.Type != ArgNumber {
			return month
		}
	}
	if cost.Number == 0 {
		return newNumberFormulaArg(0)
	}
	if calcDbArgsCompare(cost, salvage, life, period) || (month.Number < 1) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	dr := 1 - math.Pow(salvage.Number/cost.Number, 1/life.Number)
	dr = math.Round(dr*1000) / 1000
	pd, depreciation := 0.0, 0.0
	for per := 1; per <= int(period.Number); per++ {
		if per == 1 {
			depreciation = cost.Number * dr * month.Number / 12
		} else if per == int(life.Number+1) {
			depreciation = (cost.Number - pd) * dr * (12 - month.Number) / 12
		} else {
			depreciation = (cost.Number - pd) * dr
		}
		pd += depreciation
	}
	return newNumberFormulaArg(depreciation)
}

// DDB function calculates the depreciation of an asset, using the Double
// Declining Balance Method, or another specified depreciation rate. The
// syntax of the function is:
//
//	DDB(cost,salvage,life,period,[factor])
func (fn *formulaFuncs) DDB(argsList *list.List) formulaArg {
	if argsList.Len() < 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "DDB requires at least 4 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "DDB allows at most 5 arguments")
	}
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber {
		return cost
	}
	salvage := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if salvage.Type != ArgNumber {
		return salvage
	}
	life := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if life.Type != ArgNumber {
		return life
	}
	period := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if period.Type != ArgNumber {
		return period
	}
	factor := newNumberFormulaArg(2)
	if argsList.Len() == 5 {
		if factor = argsList.Back().Value.(formulaArg).ToNumber(); factor.Type != ArgNumber {
			return factor
		}
	}
	if cost.Number == 0 {
		return newNumberFormulaArg(0)
	}
	if calcDbArgsCompare(cost, salvage, life, period) || (factor.Number <= 0.0) || (period.Number > life.Number) {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	pd, depreciation := 0.0, 0.0
	for per := 1; per <= int(period.Number); per++ {
		depreciation = math.Min((cost.Number-pd)*(factor.Number/life.Number), cost.Number-salvage.Number-pd)
		pd += depreciation
	}
	return newNumberFormulaArg(depreciation)
}

// prepareDataValueArgs convert first N arguments to data value for the
// formula functions.
func (fn *formulaFuncs) prepareDataValueArgs(n int, argsList *list.List) formulaArg {
	l := list.New()
	var dataValues []formulaArg
	getDateValue := func(arg formulaArg, l *list.List) formulaArg {
		switch arg.Type {
		case ArgNumber:
			break
		case ArgString:
			num := arg.ToNumber()
			if num.Type == ArgNumber {
				arg = num
				break
			}
			l.Init()
			l.PushBack(arg)
			arg = fn.DATEVALUE(l)
			if arg.Type == ArgError {
				return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
			}
		default:
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
		return arg
	}
	for i, arg := 0, argsList.Front(); i < n; arg = arg.Next() {
		dataValue := getDateValue(arg.Value.(formulaArg), l)
		if dataValue.Type != ArgNumber {
			return dataValue
		}
		dataValues = append(dataValues, dataValue)
		i++
	}
	return newListFormulaArg(dataValues)
}

// discIntrate is an implementation of the formula functions DISC and INTRATE.
func (fn *formulaFuncs) discIntrate(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 4 && argsList.Len() != 5 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 4 or 5 arguments", name))
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity, argName := args.List[0], args.List[1], "pr"
	if maturity.Number <= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires maturity > settlement", name))
	}
	prInvestment := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if prInvestment.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if prInvestment.Number <= 0 {
		if name == "INTRATE" {
			argName = "investment"
		}
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires %s > 0", name, argName))
	}
	redemption := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if redemption.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if redemption.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires redemption > 0", name))
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	frac := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	if name == "INTRATE" {
		return newNumberFormulaArg((redemption.Number - prInvestment.Number) / prInvestment.Number / frac.Number)
	}
	return newNumberFormulaArg((redemption.Number - prInvestment.Number) / redemption.Number / frac.Number)
}

// DISC function calculates the Discount Rate for a security. The syntax of
// the function is:
//
//	DISC(settlement,maturity,pr,redemption,[basis])
func (fn *formulaFuncs) DISC(argsList *list.List) formulaArg {
	return fn.discIntrate("DISC", argsList)
}

// DOLLARDE function converts a dollar value in fractional notation, into a
// dollar value expressed as a decimal. The syntax of the function is:
//
//	DOLLARDE(fractional_dollar,fraction)
func (fn *formulaFuncs) DOLLARDE(argsList *list.List) formulaArg {
	return fn.dollar("DOLLARDE", argsList)
}

// DOLLARFR function converts a dollar value in decimal notation, into a
// dollar value that is expressed in fractional notation. The syntax of the
// function is:
//
//	DOLLARFR(decimal_dollar,fraction)
func (fn *formulaFuncs) DOLLARFR(argsList *list.List) formulaArg {
	return fn.dollar("DOLLARFR", argsList)
}

// dollar is an implementation of the formula functions DOLLARDE and DOLLARFR.
func (fn *formulaFuncs) dollar(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 2 arguments", name))
	}
	dollar := argsList.Front().Value.(formulaArg).ToNumber()
	if dollar.Type != ArgNumber {
		return dollar
	}
	frac := argsList.Back().Value.(formulaArg).ToNumber()
	if frac.Type != ArgNumber {
		return frac
	}
	if frac.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if frac.Number == 0 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	cents := math.Mod(dollar.Number, 1)
	if name == "DOLLARDE" {
		cents /= frac.Number
		cents *= math.Pow(10, math.Ceil(math.Log10(frac.Number)))
	} else {
		cents *= frac.Number
		cents *= math.Pow(10, -math.Ceil(math.Log10(frac.Number)))
	}
	return newNumberFormulaArg(math.Floor(dollar.Number) + cents)
}

// prepareDurationArgs checking and prepare arguments for the formula
// functions DURATION and MDURATION.
func (fn *formulaFuncs) prepareDurationArgs(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 5 && argsList.Len() != 6 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 5 or 6 arguments", name))
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	if settlement.Number >= maturity.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires maturity > settlement", name))
	}
	coupon := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if coupon.Type != ArgNumber {
		return coupon
	}
	if coupon.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires coupon >= 0", name))
	}
	yld := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if yld.Type != ArgNumber {
		return yld
	}
	if yld.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires yld >= 0", name))
	}
	frequency := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if frequency.Type != ArgNumber {
		return frequency
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 6 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newListFormulaArg([]formulaArg{settlement, maturity, coupon, yld, frequency, basis})
}

// duration is an implementation of the formula function DURATION.
func (fn *formulaFuncs) duration(settlement, maturity, coupon, yld, frequency, basis formulaArg) formulaArg {
	frac := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	argumments := list.New().Init()
	argumments.PushBack(settlement)
	argumments.PushBack(maturity)
	argumments.PushBack(frequency)
	argumments.PushBack(basis)
	coups := fn.COUPNUM(argumments)
	duration := 0.0
	p := 0.0
	coupon.Number *= 100 / frequency.Number
	yld.Number /= frequency.Number
	yld.Number++
	diff := frac.Number*frequency.Number - coups.Number
	for t := 1.0; t < coups.Number; t++ {
		tDiff := t + diff
		add := coupon.Number / math.Pow(yld.Number, tDiff)
		p += add
		duration += tDiff * add
	}
	add := (coupon.Number + 100) / math.Pow(yld.Number, coups.Number+diff)
	p += add
	duration += (coups.Number + diff) * add
	duration /= p
	duration /= frequency.Number
	return newNumberFormulaArg(duration)
}

// DURATION function calculates the Duration (specifically, the Macaulay
// Duration) of a security that pays periodic interest, assuming a par value
// of $100. The syntax of the function is:
//
//	DURATION(settlement,maturity,coupon,yld,frequency,[basis])
func (fn *formulaFuncs) DURATION(argsList *list.List) formulaArg {
	args := fn.prepareDurationArgs("DURATION", argsList)
	if args.Type != ArgList {
		return args
	}
	return fn.duration(args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5])
}

// EFFECT function returns the effective annual interest rate for a given
// nominal interest rate and number of compounding periods per year. The
// syntax of the function is:
//
//	EFFECT(nominal_rate,npery)
func (fn *formulaFuncs) EFFECT(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "EFFECT requires 2 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	npery := argsList.Back().Value.(formulaArg).ToNumber()
	if npery.Type != ArgNumber {
		return npery
	}
	if rate.Number <= 0 || npery.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(math.Pow(1+rate.Number/npery.Number, npery.Number) - 1)
}

// EUROCONVERT function convert a number to euro or from euro to a
// participating currency. You can also use it to convert a number from one
// participating currency to another by using the euro as an intermediary
// (triangulation). The syntax of the function is:
//
//	EUROCONVERT(number,sourcecurrency,targetcurrency[,fullprecision,triangulationprecision])
func (fn *formulaFuncs) EUROCONVERT(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "EUROCONVERT requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "EUROCONVERT allows at most 5 arguments")
	}
	number := argsList.Front().Value.(formulaArg).ToNumber()
	if number.Type != ArgNumber {
		return number
	}
	sourceCurrency := argsList.Front().Next().Value.(formulaArg).Value()
	targetCurrency := argsList.Front().Next().Next().Value.(formulaArg).Value()
	fullPrec, triangulationPrec := newBoolFormulaArg(false), newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if fullPrec = argsList.Front().Next().Next().Next().Value.(formulaArg).ToBool(); fullPrec.Type != ArgNumber {
			return fullPrec
		}
	}
	if argsList.Len() == 5 {
		if triangulationPrec = argsList.Back().Value.(formulaArg).ToNumber(); triangulationPrec.Type != ArgNumber {
			return triangulationPrec
		}
	}
	convertTable := map[string][]float64{
		"EUR": {1.0, 2},
		"ATS": {13.7603, 2},
		"BEF": {40.3399, 0},
		"DEM": {1.95583, 2},
		"ESP": {166.386, 0},
		"FIM": {5.94573, 2},
		"FRF": {6.55957, 2},
		"IEP": {0.787564, 2},
		"ITL": {1936.27, 0},
		"LUF": {40.3399, 0},
		"NLG": {2.20371, 2},
		"PTE": {200.482, 2},
		"GRD": {340.750, 2},
		"SIT": {239.640, 2},
		"MTL": {0.429300, 2},
		"CYP": {0.585274, 2},
		"SKK": {30.1260, 2},
		"EEK": {15.6466, 2},
		"LVL": {0.702804, 2},
		"LTL": {3.45280, 2},
	}
	source, ok := convertTable[sourceCurrency]
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	target, ok := convertTable[targetCurrency]
	if !ok {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if sourceCurrency == targetCurrency {
		return number
	}
	var res float64
	if sourceCurrency == "EUR" {
		res = number.Number * target[0]
	} else {
		intermediate := number.Number / source[0]
		if triangulationPrec.Number != 0 {
			ratio := math.Pow(10, triangulationPrec.Number)
			intermediate = math.Round(intermediate*ratio) / ratio
		}
		res = intermediate * target[0]
	}
	if fullPrec.Number != 1 {
		ratio := math.Pow(10, target[1])
		res = math.Round(res*ratio) / ratio
	}
	return newNumberFormulaArg(res)
}

// FV function calculates the Future Value of an investment with periodic
// constant payments and a constant interest rate. The syntax of the function
// is:
//
//	FV(rate,nper,[pmt],[pv],[type])
func (fn *formulaFuncs) FV(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "FV requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "FV allows at most 5 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	nper := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pmt := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pmt.Type != ArgNumber {
		return pmt
	}
	pv, typ := newNumberFormulaArg(0), newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if pv = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); pv.Type != ArgNumber {
			return pv
		}
	}
	if argsList.Len() == 5 {
		if typ = argsList.Back().Value.(formulaArg).ToNumber(); typ.Type != ArgNumber {
			return typ
		}
	}
	if typ.Number != 0 && typ.Number != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if rate.Number != 0 {
		return newNumberFormulaArg(-pv.Number*math.Pow(1+rate.Number, nper.Number) - pmt.Number*(1+rate.Number*typ.Number)*(math.Pow(1+rate.Number, nper.Number)-1)/rate.Number)
	}
	return newNumberFormulaArg(-pv.Number - pmt.Number*nper.Number)
}

// FVSCHEDULE function calculates the Future Value of an investment with a
// variable interest rate. The syntax of the function is:
//
//	FVSCHEDULE(principal,schedule)
func (fn *formulaFuncs) FVSCHEDULE(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "FVSCHEDULE requires 2 arguments")
	}
	pri := argsList.Front().Value.(formulaArg).ToNumber()
	if pri.Type != ArgNumber {
		return pri
	}
	principal := pri.Number
	for _, arg := range argsList.Back().Value.(formulaArg).ToList() {
		if arg.Value() == "" {
			continue
		}
		rate := arg.ToNumber()
		if rate.Type != ArgNumber {
			return rate
		}
		principal *= 1 + rate.Number
	}
	return newNumberFormulaArg(principal)
}

// INTRATE function calculates the interest rate for a fully invested
// security. The syntax of the function is:
//
//	INTRATE(settlement,maturity,investment,redemption,[basis])
func (fn *formulaFuncs) INTRATE(argsList *list.List) formulaArg {
	return fn.discIntrate("INTRATE", argsList)
}

// IPMT function calculates the interest payment, during a specific period of a
// loan or investment that is paid in constant periodic payments, with a
// constant interest rate. The syntax of the function is:
//
//	IPMT(rate,per,nper,pv,[fv],[type])
func (fn *formulaFuncs) IPMT(argsList *list.List) formulaArg {
	return fn.ipmt("IPMT", argsList)
}

// calcIpmt is part of the implementation ipmt.
func calcIpmt(name string, typ, per, pmt, pv, rate formulaArg) formulaArg {
	capital, interest, principal := pv.Number, 0.0, 0.0
	for i := 1; i <= int(per.Number); i++ {
		if typ.Number != 0 && i == 1 {
			interest = 0
		} else {
			interest = -capital * rate.Number
		}
		principal = pmt.Number - interest
		capital += principal
	}
	if name == "IPMT" {
		return newNumberFormulaArg(interest)
	}
	return newNumberFormulaArg(principal)
}

// ipmt is an implementation of the formula functions IPMT and PPMT.
func (fn *formulaFuncs) ipmt(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 4 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 4 arguments", name))
	}
	if argsList.Len() > 6 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 6 arguments", name))
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	per := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if per.Type != ArgNumber {
		return per
	}
	nper := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pv := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	fv, typ := newNumberFormulaArg(0), newNumberFormulaArg(0)
	if argsList.Len() >= 5 {
		if fv = argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); fv.Type != ArgNumber {
			return fv
		}
	}
	if argsList.Len() == 6 {
		if typ = argsList.Back().Value.(formulaArg).ToNumber(); typ.Type != ArgNumber {
			return typ
		}
	}
	if typ.Number != 0 && typ.Number != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if per.Number <= 0 || per.Number > nper.Number {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	args := list.New().Init()
	args.PushBack(rate)
	args.PushBack(nper)
	args.PushBack(pv)
	args.PushBack(fv)
	args.PushBack(typ)
	pmt := fn.PMT(args)
	return calcIpmt(name, typ, per, pmt, pv, rate)
}

// IRR function returns the Internal Rate of Return for a supplied series of
// periodic cash flows (i.e. an initial investment value and a series of net
// income values). The syntax of the function is:
//
//	IRR(values,[guess])
func (fn *formulaFuncs) IRR(argsList *list.List) formulaArg {
	if argsList.Len() < 1 {
		return newErrorFormulaArg(formulaErrorVALUE, "IRR requires at least 1 argument")
	}
	if argsList.Len() > 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "IRR allows at most 2 arguments")
	}
	values, guess := argsList.Front().Value.(formulaArg).ToList(), newNumberFormulaArg(0.1)
	if argsList.Len() > 1 {
		if guess = argsList.Back().Value.(formulaArg).ToNumber(); guess.Type != ArgNumber {
			return guess
		}
	}
	x1, x2 := newNumberFormulaArg(0), guess
	args := list.New().Init()
	args.PushBack(x1)
	for _, v := range values {
		args.PushBack(v)
	}
	f1 := fn.NPV(args)
	args.Front().Value = x2
	f2 := fn.NPV(args)
	for i := 0; i < maxFinancialIterations; i++ {
		if f1.Number*f2.Number < 0 {
			break
		}
		if math.Abs(f1.Number) < math.Abs(f2.Number) {
			x1.Number += 1.6 * (x1.Number - x2.Number)
			args.Front().Value = x1
			f1 = fn.NPV(args)
			continue
		}
		x2.Number += 1.6 * (x2.Number - x1.Number)
		args.Front().Value = x2
		f2 = fn.NPV(args)
	}
	if f1.Number*f2.Number > 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	args.Front().Value = x1
	f := fn.NPV(args)
	var rtb, dx, xMid, fMid float64
	if f.Number < 0 {
		rtb = x1.Number
		dx = x2.Number - x1.Number
	} else {
		rtb = x2.Number
		dx = x1.Number - x2.Number
	}
	for i := 0; i < maxFinancialIterations; i++ {
		dx *= 0.5
		xMid = rtb + dx
		args.Front().Value = newNumberFormulaArg(xMid)
		fMid = fn.NPV(args).Number
		if fMid <= 0 {
			rtb = xMid
		}
		if math.Abs(fMid) < financialPrecision || math.Abs(dx) < financialPrecision {
			break
		}
	}
	return newNumberFormulaArg(xMid)
}

// ISPMT function calculates the interest paid during a specific period of a
// loan or investment. The syntax of the function is:
//
//	ISPMT(rate,per,nper,pv)
func (fn *formulaFuncs) ISPMT(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "ISPMT requires 4 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	per := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if per.Type != ArgNumber {
		return per
	}
	nper := argsList.Back().Prev().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pv := argsList.Back().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	pr, payment, num := pv.Number, pv.Number/nper.Number, 0.0
	for i := 0; i <= int(per.Number); i++ {
		num = rate.Number * pr * -1
		pr -= payment
		if i == int(nper.Number) {
			num = 0
		}
	}
	return newNumberFormulaArg(num)
}

// MDURATION function calculates the Modified Macaulay Duration of a security
// that pays periodic interest, assuming a par value of $100. The syntax of
// the function is:
//
//	MDURATION(settlement,maturity,coupon,yld,frequency,[basis])
func (fn *formulaFuncs) MDURATION(argsList *list.List) formulaArg {
	args := fn.prepareDurationArgs("MDURATION", argsList)
	if args.Type != ArgList {
		return args
	}
	duration := fn.duration(args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5])
	if duration.Type != ArgNumber {
		return duration
	}
	return newNumberFormulaArg(duration.Number / (1 + args.List[3].Number/args.List[4].Number))
}

// MIRR function returns the Modified Internal Rate of Return for a supplied
// series of periodic cash flows (i.e. a set of values, which includes an
// initial investment value and a series of net income values). The syntax of
// the function is:
//
//	MIRR(values,finance_rate,reinvest_rate)
func (fn *formulaFuncs) MIRR(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "MIRR requires 3 arguments")
	}
	values := argsList.Front().Value.(formulaArg).ToList()
	financeRate := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if financeRate.Type != ArgNumber {
		return financeRate
	}
	reinvestRate := argsList.Back().Value.(formulaArg).ToNumber()
	if reinvestRate.Type != ArgNumber {
		return reinvestRate
	}
	n, fr, rr, npvPos, npvNeg := len(values), 1+financeRate.Number, 1+reinvestRate.Number, 0.0, 0.0
	for i, v := range values {
		val := v.ToNumber()
		if val.Number >= 0 {
			npvPos += val.Number / math.Pow(rr, float64(i))
			continue
		}
		npvNeg += val.Number / math.Pow(fr, float64(i))
	}
	if npvNeg == 0 || npvPos == 0 || reinvestRate.Number <= -1 {
		return newErrorFormulaArg(formulaErrorDIV, formulaErrorDIV)
	}
	return newNumberFormulaArg(math.Pow(-npvPos*math.Pow(rr, float64(n))/(npvNeg*rr), 1/(float64(n)-1)) - 1)
}

// NOMINAL function returns the nominal interest rate for a given effective
// interest rate and number of compounding periods per year. The syntax of
// the function is:
//
//	NOMINAL(effect_rate,npery)
func (fn *formulaFuncs) NOMINAL(argsList *list.List) formulaArg {
	if argsList.Len() != 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "NOMINAL requires 2 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	npery := argsList.Back().Value.(formulaArg).ToNumber()
	if npery.Type != ArgNumber {
		return npery
	}
	if rate.Number <= 0 || npery.Number < 1 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(npery.Number * (math.Pow(rate.Number+1, 1/npery.Number) - 1))
}

// NPER function calculates the number of periods required to pay off a loan,
// for a constant periodic payment and a constant interest rate. The syntax
// of the function is:
//
//	NPER(rate,pmt,pv,[fv],[type])
func (fn *formulaFuncs) NPER(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "NPER requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "NPER allows at most 5 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	pmt := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if pmt.Type != ArgNumber {
		return pmt
	}
	pv := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	fv, typ := newNumberFormulaArg(0), newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if fv = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); fv.Type != ArgNumber {
			return fv
		}
	}
	if argsList.Len() == 5 {
		if typ = argsList.Back().Value.(formulaArg).ToNumber(); typ.Type != ArgNumber {
			return typ
		}
	}
	if typ.Number != 0 && typ.Number != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if pmt.Number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if rate.Number != 0 {
		p := math.Log((pmt.Number*(1+rate.Number*typ.Number)/rate.Number-fv.Number)/(pv.Number+pmt.Number*(1+rate.Number*typ.Number)/rate.Number)) / math.Log(1+rate.Number)
		return newNumberFormulaArg(p)
	}
	return newNumberFormulaArg((-pv.Number - fv.Number) / pmt.Number)
}

// NPV function calculates the Net Present Value of an investment, based on a
// supplied discount rate, and a series of future payments and income. The
// syntax of the function is:
//
//	NPV(rate,value1,[value2],[value3],...)
func (fn *formulaFuncs) NPV(argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, "NPV requires at least 2 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	val, i := 0.0, 1
	for arg := argsList.Front().Next(); arg != nil; arg = arg.Next() {
		num := arg.Value.(formulaArg).ToNumber()
		if num.Type != ArgNumber {
			continue
		}
		val += num.Number / math.Pow(1+rate.Number, float64(i))
		i++
	}
	return newNumberFormulaArg(val)
}

// aggrBetween is a part of implementation of the formula function ODDFPRICE.
func aggrBetween(startPeriod, endPeriod float64, initialValue []float64, f func(acc []float64, index float64) []float64) []float64 {
	var s []float64
	if startPeriod <= endPeriod {
		for i := startPeriod; i <= endPeriod; i++ {
			s = append(s, i)
		}
	} else {
		for i := startPeriod; i >= endPeriod; i-- {
			s = append(s, i)
		}
	}
	return fold(f, initialValue, s)
}

// fold is a part of implementation of the formula function ODDFPRICE.
func fold(f func(acc []float64, index float64) []float64, state []float64, source []float64) []float64 {
	length, value := len(source), state
	for index := 0; length > index; index++ {
		value = f(value, source[index])
	}
	return value
}

// changeMonth is a part of implementation of the formula function ODDFPRICE.
func changeMonth(date time.Time, numMonths float64, returnLastMonth bool) time.Time {
	offsetDay := 0
	if returnLastMonth && date.Day() == getDaysInMonth(date.Year(), int(date.Month())) {
		offsetDay--
	}
	newDate := date.AddDate(0, int(numMonths), offsetDay)
	if returnLastMonth {
		lastDay := getDaysInMonth(newDate.Year(), int(newDate.Month()))
		return timeFromExcelTime(daysBetween(excelMinTime1900.Unix(), makeDate(newDate.Year(), newDate.Month(), lastDay))+1, false)
	}
	return newDate
}

// datesAggregate is a part of implementation of the formula function
// ODDFPRICE.
func datesAggregate(startDate, endDate time.Time, numMonths float64, f func(pcd, ncd time.Time) float64, acc float64, returnLastMonth bool) (time.Time, time.Time, float64) {
	frontDate, trailingDate := startDate, endDate
	s1 := frontDate.After(endDate) || frontDate.Equal(endDate)
	s2 := endDate.After(frontDate) || endDate.Equal(frontDate)
	stop := s2
	if numMonths > 0 {
		stop = s1
	}
	for !stop {
		trailingDate = frontDate
		frontDate = changeMonth(frontDate, numMonths, returnLastMonth)
		fn := f(frontDate, trailingDate)
		acc += fn
		s1 = frontDate.After(endDate) || frontDate.Equal(endDate)
		s2 = endDate.After(frontDate) || endDate.Equal(frontDate)
		stop = s2
		if numMonths > 0 {
			stop = s1
		}
	}
	return frontDate, trailingDate, acc
}

// coupNumber is a part of implementation of the formula function ODDFPRICE.
func coupNumber(maturity, settlement, numMonths float64) float64 {
	maturityTime, settlementTime := timeFromExcelTime(maturity, false), timeFromExcelTime(settlement, false)
	my, mm, md := maturityTime.Year(), maturityTime.Month(), maturityTime.Day()
	sy, sm, sd := settlementTime.Year(), settlementTime.Month(), settlementTime.Day()
	couponsTemp, endOfMonthTemp := 0.0, getDaysInMonth(my, int(mm)) == md
	endOfMonth := endOfMonthTemp
	if !endOfMonthTemp && mm != 2 && md > 28 && md < getDaysInMonth(my, int(mm)) {
		endOfMonth = getDaysInMonth(sy, int(sm)) == sd
	}
	startDate := changeMonth(settlementTime, 0, endOfMonth)
	coupons := couponsTemp
	if startDate.After(settlementTime) {
		coupons++
	}
	date := changeMonth(startDate, numMonths, endOfMonth)
	f := func(pcd, ncd time.Time) float64 {
		return 1
	}
	_, _, result := datesAggregate(date, maturityTime, numMonths, f, coupons, endOfMonth)
	return result
}

// prepareOddYldOrPrArg checking and prepare yield or price arguments for the
// formula functions ODDFPRICE, ODDFYIELD, ODDLPRICE and ODDLYIELD.
func prepareOddYldOrPrArg(name string, arg formulaArg) formulaArg {
	yldOrPr := arg.ToNumber()
	if yldOrPr.Type != ArgNumber {
		return yldOrPr
	}
	if (name == "ODDFPRICE" || name == "ODDLPRICE") && yldOrPr.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires yld >= 0", name))
	}
	if (name == "ODDFYIELD" || name == "ODDLYIELD") && yldOrPr.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires pr > 0", name))
	}
	return yldOrPr
}

// prepareOddfArgs checking and prepare arguments for the formula
// functions ODDFPRICE and ODDFYIELD.
func (fn *formulaFuncs) prepareOddfArgs(name string, argsList *list.List) formulaArg {
	dateValues := fn.prepareDataValueArgs(4, argsList)
	if dateValues.Type != ArgList {
		return dateValues
	}
	settlement, maturity, issue, firstCoupon := dateValues.List[0], dateValues.List[1], dateValues.List[2], dateValues.List[3]
	if issue.Number >= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires settlement > issue", name))
	}
	if settlement.Number >= firstCoupon.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires first_coupon > settlement", name))
	}
	if firstCoupon.Number >= maturity.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires maturity > first_coupon", name))
	}
	rate := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires rate >= 0", name))
	}
	yldOrPr := prepareOddYldOrPrArg(name, argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg))
	if yldOrPr.Type != ArgNumber {
		return yldOrPr
	}
	redemption := argsList.Front().Next().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if redemption.Type != ArgNumber {
		return redemption
	}
	if redemption.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires redemption > 0", name))
	}
	frequency := argsList.Front().Next().Next().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if frequency.Type != ArgNumber {
		return frequency
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 9 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newListFormulaArg([]formulaArg{settlement, maturity, issue, firstCoupon, rate, yldOrPr, redemption, frequency, basis})
}

// ODDFPRICE function calculates the price per $100 face value of a security
// with an odd (short or long) first period. The syntax of the function is:
//
//	ODDFPRICE(settlement,maturity,issue,first_coupon,rate,yld,redemption,frequency,[basis])
func (fn *formulaFuncs) ODDFPRICE(argsList *list.List) formulaArg {
	if argsList.Len() != 8 && argsList.Len() != 9 {
		return newErrorFormulaArg(formulaErrorVALUE, "ODDFPRICE requires 8 or 9 arguments")
	}
	args := fn.prepareOddfArgs("ODDFPRICE", argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity, issue, firstCoupon, rate, yld, redemption, frequency, basisArg := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5], args.List[6], args.List[7], args.List[8]
	if basisArg.Number < 0 || basisArg.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, "invalid basis")
	}
	issueTime := timeFromExcelTime(issue.Number, false)
	settlementTime := timeFromExcelTime(settlement.Number, false)
	maturityTime := timeFromExcelTime(maturity.Number, false)
	firstCouponTime := timeFromExcelTime(firstCoupon.Number, false)
	basis := int(basisArg.Number)
	monthDays := getDaysInMonth(maturityTime.Year(), int(maturityTime.Month()))
	returnLastMonth := monthDays == maturityTime.Day()
	numMonths := 12 / frequency.Number
	numMonthsNeg := -numMonths
	mat := changeMonth(maturityTime, numMonthsNeg, returnLastMonth)
	pcd, _, _ := datesAggregate(mat, firstCouponTime, numMonthsNeg, func(d1, d2 time.Time) float64 {
		return 0
	}, 0, returnLastMonth)
	if !pcd.Equal(firstCouponTime) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	fnArgs := list.New().Init()
	fnArgs.PushBack(settlement)
	fnArgs.PushBack(maturity)
	fnArgs.PushBack(frequency)
	fnArgs.PushBack(basisArg)
	e := fn.COUPDAYS(fnArgs)
	n := fn.COUPNUM(fnArgs)
	m := frequency.Number
	dfc := coupdays(issueTime, firstCouponTime, basis)
	if dfc < e.Number {
		dsc := coupdays(settlementTime, firstCouponTime, basis)
		a := coupdays(issueTime, settlementTime, basis)
		x := yld.Number/m + 1
		y := dsc / e.Number
		p1 := x
		p3 := math.Pow(p1, n.Number-1+y)
		term1 := redemption.Number / p3
		term2 := 100 * rate.Number / m * dfc / e.Number / math.Pow(p1, y)
		f := func(acc []float64, index float64) []float64 {
			return []float64{acc[0] + 100*rate.Number/m/math.Pow(p1, index-1+y)}
		}
		term3 := aggrBetween(2, math.Floor(n.Number), []float64{0}, f)
		p2 := rate.Number / m
		term4 := a / e.Number * p2 * 100
		return newNumberFormulaArg(term1 + term2 + term3[0] - term4)
	}
	fnArgs.Init()
	fnArgs.PushBack(issue)
	fnArgs.PushBack(firstCoupon)
	fnArgs.PushBack(frequency)
	nc := fn.COUPNUM(fnArgs)
	lastCoupon := firstCoupon.Number
	aggrFunc := func(acc []float64, index float64) []float64 {
		lastCouponTime := timeFromExcelTime(lastCoupon, false)
		earlyCoupon := daysBetween(excelMinTime1900.Unix(), makeDate(lastCouponTime.Year(), time.Month(float64(lastCouponTime.Month())+numMonthsNeg), lastCouponTime.Day())) + 1
		earlyCouponTime := timeFromExcelTime(earlyCoupon, false)
		nl := e.Number
		if basis == 1 {
			nl = coupdays(earlyCouponTime, lastCouponTime, basis)
		}
		dci := coupdays(issueTime, lastCouponTime, basis)
		if index > 1 {
			dci = nl
		}
		startDate := earlyCoupon
		if issue.Number > earlyCoupon {
			startDate = issue.Number
		}
		endDate := lastCoupon
		if settlement.Number < lastCoupon {
			endDate = settlement.Number
		}
		startDateTime := timeFromExcelTime(startDate, false)
		endDateTime := timeFromExcelTime(endDate, false)
		a := coupdays(startDateTime, endDateTime, basis)
		lastCoupon = earlyCoupon
		dcnl := acc[0]
		anl := acc[1]
		return []float64{dcnl + dci/nl, anl + a/nl}
	}
	ag := aggrBetween(math.Floor(nc.Number), 1, []float64{0, 0}, aggrFunc)
	dcnl, anl := ag[0], ag[1]
	dsc := 0.0
	fnArgs.Init()
	fnArgs.PushBack(settlement)
	fnArgs.PushBack(firstCoupon)
	fnArgs.PushBack(frequency)
	if basis == 2 || basis == 3 {
		d := timeFromExcelTime(fn.COUPNCD(fnArgs).Number, false)
		dsc = coupdays(settlementTime, d, basis)
	} else {
		d := timeFromExcelTime(fn.COUPPCD(fnArgs).Number, false)
		a := coupdays(d, settlementTime, basis)
		dsc = e.Number - a
	}
	nq := coupNumber(firstCoupon.Number, settlement.Number, numMonths)
	fnArgs.Init()
	fnArgs.PushBack(firstCoupon)
	fnArgs.PushBack(maturity)
	fnArgs.PushBack(frequency)
	fnArgs.PushBack(basisArg)
	n = fn.COUPNUM(fnArgs)
	x := yld.Number/m + 1
	y := dsc / e.Number
	p1 := x
	p3 := math.Pow(p1, y+nq+n.Number)
	term1 := redemption.Number / p3
	term2 := 100 * rate.Number / m * dcnl / math.Pow(p1, nq+y)
	f := func(acc []float64, index float64) []float64 {
		return []float64{acc[0] + 100*rate.Number/m/math.Pow(p1, index+nq+y)}
	}
	term3 := aggrBetween(1, math.Floor(n.Number), []float64{0}, f)
	term4 := 100 * rate.Number / m * anl
	return newNumberFormulaArg(term1 + term2 + term3[0] - term4)
}

// getODDFPRICE is a part of implementation of the formula function ODDFPRICE.
func getODDFPRICE(f func(yld float64) float64, x, cnt, prec float64) float64 {
	const maxCnt = 20.0
	d := func(f func(yld float64) float64, x float64) float64 {
		return (f(x+prec) - f(x-prec)) / (2 * prec)
	}
	fx, Fx := f(x), d(f, x)
	newX := x - (fx / Fx)
	if math.Abs(newX-x) < prec {
		return newX
	} else if cnt > maxCnt {
		return newX
	}
	return getODDFPRICE(f, newX, cnt+1, prec)
}

// ODDFYIELD function calculates the yield of a security with an odd (short or
// long) first period. The syntax of the function is:
//
//	ODDFYIELD(settlement,maturity,issue,first_coupon,rate,pr,redemption,frequency,[basis])
func (fn *formulaFuncs) ODDFYIELD(argsList *list.List) formulaArg {
	if argsList.Len() != 8 && argsList.Len() != 9 {
		return newErrorFormulaArg(formulaErrorVALUE, "ODDFYIELD requires 8 or 9 arguments")
	}
	args := fn.prepareOddfArgs("ODDFYIELD", argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity, issue, firstCoupon, rate, pr, redemption, frequency, basisArg := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5], args.List[6], args.List[7], args.List[8]
	if basisArg.Number < 0 || basisArg.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, "invalid basis")
	}
	settlementTime := timeFromExcelTime(settlement.Number, false)
	maturityTime := timeFromExcelTime(maturity.Number, false)
	years := coupdays(settlementTime, maturityTime, int(basisArg.Number))
	px := pr.Number - 100
	num := rate.Number*years*100 - px
	denum := px/4 + years*px/2 + years*100
	guess := num / denum
	f := func(yld float64) float64 {
		fnArgs := list.New().Init()
		fnArgs.PushBack(settlement)
		fnArgs.PushBack(maturity)
		fnArgs.PushBack(issue)
		fnArgs.PushBack(firstCoupon)
		fnArgs.PushBack(rate)
		fnArgs.PushBack(newNumberFormulaArg(yld))
		fnArgs.PushBack(redemption)
		fnArgs.PushBack(frequency)
		fnArgs.PushBack(basisArg)
		return pr.Number - fn.ODDFPRICE(fnArgs).Number
	}
	if result := getODDFPRICE(f, guess, 0, 1e-7); !math.IsInf(result, 0) {
		return newNumberFormulaArg(result)
	}
	return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
}

// prepareOddlArgs checking and prepare arguments for the formula
// functions ODDLPRICE and ODDLYIELD.
func (fn *formulaFuncs) prepareOddlArgs(name string, argsList *list.List) formulaArg {
	dateValues := fn.prepareDataValueArgs(3, argsList)
	if dateValues.Type != ArgList {
		return dateValues
	}
	settlement, maturity, lastInterest := dateValues.List[0], dateValues.List[1], dateValues.List[2]
	if lastInterest.Number >= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires settlement > last_interest", name))
	}
	if settlement.Number >= maturity.Number {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires maturity > settlement", name))
	}
	rate := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires rate >= 0", name))
	}
	yldOrPr := prepareOddYldOrPrArg(name, argsList.Front().Next().Next().Next().Next().Value.(formulaArg))
	if yldOrPr.Type != ArgNumber {
		return yldOrPr
	}
	redemption := argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if redemption.Type != ArgNumber {
		return redemption
	}
	if redemption.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires redemption > 0", name))
	}
	frequency := argsList.Front().Next().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if frequency.Type != ArgNumber {
		return frequency
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 8 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return newListFormulaArg([]formulaArg{settlement, maturity, lastInterest, rate, yldOrPr, redemption, frequency, basis})
}

// oddl is an implementation of the formula functions ODDLPRICE and ODDLYIELD.
func (fn *formulaFuncs) oddl(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 7 && argsList.Len() != 8 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 7 or 8 arguments", name))
	}
	args := fn.prepareOddlArgs(name, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity, lastInterest, rate, prOrYld, redemption, frequency, basisArg := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5], args.List[6], args.List[7]
	if basisArg.Number < 0 || basisArg.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, "invalid basis")
	}
	settlementTime := timeFromExcelTime(settlement.Number, false)
	maturityTime := timeFromExcelTime(maturity.Number, false)
	basis := int(basisArg.Number)
	numMonths := 12 / frequency.Number
	fnArgs := list.New().Init()
	fnArgs.PushBack(lastInterest)
	fnArgs.PushBack(maturity)
	fnArgs.PushBack(frequency)
	fnArgs.PushBack(basisArg)
	nc := fn.COUPNUM(fnArgs)
	earlyCoupon := lastInterest.Number
	aggrFunc := func(acc []float64, index float64) []float64 {
		earlyCouponTime := timeFromExcelTime(earlyCoupon, false)
		lateCouponTime := changeMonth(earlyCouponTime, numMonths, false)
		lateCoupon, _ := timeToExcelTime(lateCouponTime, false)
		nl := coupdays(earlyCouponTime, lateCouponTime, basis)
		dci := coupdays(earlyCouponTime, maturityTime, basis)
		if index < nc.Number {
			dci = nl
		}
		var a float64
		if lateCoupon < settlement.Number {
			a = dci
		} else if earlyCoupon < settlement.Number {
			a = coupdays(earlyCouponTime, settlementTime, basis)
		}
		startDate := earlyCoupon
		if settlement.Number > earlyCoupon {
			startDate = settlement.Number
		}
		endDate := lateCoupon
		if maturity.Number < lateCoupon {
			endDate = maturity.Number
		}
		startDateTime := timeFromExcelTime(startDate, false)
		endDateTime := timeFromExcelTime(endDate, false)
		dsc := coupdays(startDateTime, endDateTime, basis)
		earlyCoupon = lateCoupon
		dcnl := acc[0]
		anl := acc[1]
		dscnl := acc[2]
		return []float64{dcnl + dci/nl, anl + a/nl, dscnl + dsc/nl}
	}
	ag := aggrBetween(1, math.Floor(nc.Number), []float64{0, 0, 0}, aggrFunc)
	dcnl, anl, dscnl := ag[0], ag[1], ag[2]
	x := 100.0 * rate.Number / frequency.Number
	term1 := dcnl*x + redemption.Number
	if name == "ODDLPRICE" {
		term2 := dscnl*prOrYld.Number/frequency.Number + 1
		term3 := anl * x
		return newNumberFormulaArg(term1/term2 - term3)
	}
	term2 := anl*x + prOrYld.Number
	term3 := frequency.Number / dscnl
	return newNumberFormulaArg((term1 - term2) / term2 * term3)
}

// ODDLPRICE function calculates the price per $100 face value of a security
// with an odd (short or long) last period. The syntax of the function is:
//
//	ODDLPRICE(settlement,maturity,last_interest,rate,yld,redemption,frequency,[basis])
func (fn *formulaFuncs) ODDLPRICE(argsList *list.List) formulaArg {
	return fn.oddl("ODDLPRICE", argsList)
}

// ODDLYIELD function calculates the yield of a security with an odd (short or
// long) last period. The syntax of the function is:
//
//	ODDLYIELD(settlement,maturity,last_interest,rate,pr,redemption,frequency,[basis])
func (fn *formulaFuncs) ODDLYIELD(argsList *list.List) formulaArg {
	return fn.oddl("ODDLYIELD", argsList)
}

// PDURATION function calculates the number of periods required for an
// investment to reach a specified future value. The syntax of the function
// is:
//
//	PDURATION(rate,pv,fv)
func (fn *formulaFuncs) PDURATION(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "PDURATION requires 3 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	pv := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	fv := argsList.Back().Value.(formulaArg).ToNumber()
	if fv.Type != ArgNumber {
		return fv
	}
	if rate.Number <= 0 || pv.Number <= 0 || fv.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg((math.Log(fv.Number) - math.Log(pv.Number)) / math.Log(1+rate.Number))
}

// PMT function calculates the constant periodic payment required to pay off
// (or partially pay off) a loan or investment, with a constant interest
// rate, over a specified period. The syntax of the function is:
//
//	PMT(rate,nper,pv,[fv],[type])
func (fn *formulaFuncs) PMT(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "PMT requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "PMT allows at most 5 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	nper := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pv := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	fv, typ := newNumberFormulaArg(0), newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if fv = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); fv.Type != ArgNumber {
			return fv
		}
	}
	if argsList.Len() == 5 {
		if typ = argsList.Back().Value.(formulaArg).ToNumber(); typ.Type != ArgNumber {
			return typ
		}
	}
	if typ.Number != 0 && typ.Number != 1 {
		return newErrorFormulaArg(formulaErrorNA, formulaErrorNA)
	}
	if rate.Number != 0 {
		p := (-fv.Number - pv.Number*math.Pow(1+rate.Number, nper.Number)) / (1 + rate.Number*typ.Number) / ((math.Pow(1+rate.Number, nper.Number) - 1) / rate.Number)
		return newNumberFormulaArg(p)
	}
	return newNumberFormulaArg((-pv.Number - fv.Number) / nper.Number)
}

// PPMT function calculates the payment on the principal, during a specific
// period of a loan or investment that is paid in constant periodic payments,
// with a constant interest rate. The syntax of the function is:
//
//	PPMT(rate,per,nper,pv,[fv],[type])
func (fn *formulaFuncs) PPMT(argsList *list.List) formulaArg {
	return fn.ipmt("PPMT", argsList)
}

// price is an implementation of the formula function PRICE.
func (fn *formulaFuncs) price(settlement, maturity, rate, yld, redemption, frequency, basis formulaArg) formulaArg {
	if basis.Number < 0 || basis.Number > 4 {
		return newErrorFormulaArg(formulaErrorNUM, "invalid basis")
	}
	argsList := list.New().Init()
	argsList.PushBack(settlement)
	argsList.PushBack(maturity)
	argsList.PushBack(frequency)
	argsList.PushBack(basis)
	e := fn.COUPDAYS(argsList)
	dsc := fn.COUPDAYSNC(argsList).Number / e.Number
	n := fn.COUPNUM(argsList)
	a := fn.COUPDAYBS(argsList)
	ret := 0.0
	if n.Number > 1 {
		ret = redemption.Number / math.Pow(1+yld.Number/frequency.Number, n.Number-1+dsc)
		ret -= 100 * rate.Number / frequency.Number * a.Number / e.Number
		t1 := 100 * rate.Number / frequency.Number
		t2 := 1 + yld.Number/frequency.Number
		for k := 0.0; k < n.Number; k++ {
			ret += t1 / math.Pow(t2, k+dsc)
		}
	} else {
		dsc = e.Number - a.Number
		t1 := 100*(rate.Number/frequency.Number) + redemption.Number
		t2 := (yld.Number/frequency.Number)*(dsc/e.Number) + 1
		t3 := 100 * (rate.Number / frequency.Number) * (a.Number / e.Number)
		ret = t1/t2 - t3
	}
	return newNumberFormulaArg(ret)
}

// checkPriceYieldArgs checking and prepare arguments for the formula functions
// PRICE and YIELD.
func checkPriceYieldArgs(name string, rate, prYld, redemption, frequency formulaArg) formulaArg {
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, fmt.Sprintf("%s requires rate >= 0", name))
	}
	if prYld.Type != ArgNumber {
		return prYld
	}
	if redemption.Type != ArgNumber {
		return redemption
	}
	if name == "PRICE" {
		if prYld.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, "PRICE requires yld >= 0")
		}
		if redemption.Number <= 0 {
			return newErrorFormulaArg(formulaErrorNUM, "PRICE requires redemption > 0")
		}
	}
	if name == "YIELD" {
		if prYld.Number <= 0 {
			return newErrorFormulaArg(formulaErrorNUM, "YIELD requires pr > 0")
		}
		if redemption.Number < 0 {
			return newErrorFormulaArg(formulaErrorNUM, "YIELD requires redemption >= 0")
		}
	}
	if frequency.Type != ArgNumber {
		return frequency
	}
	if !validateFrequency(frequency.Number) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newEmptyFormulaArg()
}

// priceYield is an implementation of the formula functions PRICE and YIELD.
func (fn *formulaFuncs) priceYield(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 6 && argsList.Len() != 7 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 6 or 7 arguments", name))
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	rate := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	prYld := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	redemption := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	frequency := argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if arg := checkPriceYieldArgs(name, rate, prYld, redemption, frequency); arg.Type != ArgEmpty {
		return arg
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 7 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	if name == "PRICE" {
		return fn.price(settlement, maturity, rate, prYld, redemption, frequency, basis)
	}
	return fn.yield(settlement, maturity, rate, prYld, redemption, frequency, basis)
}

// PRICE function calculates the price, per $100 face value of a security that
// pays periodic interest. The syntax of the function is:
//
//	PRICE(settlement,maturity,rate,yld,redemption,frequency,[basis])
func (fn *formulaFuncs) PRICE(argsList *list.List) formulaArg {
	return fn.priceYield("PRICE", argsList)
}

// PRICEDISC function calculates the price, per $100 face value of a
// discounted security. The syntax of the function is:
//
//	PRICEDISC(settlement,maturity,discount,redemption,[basis])
func (fn *formulaFuncs) PRICEDISC(argsList *list.List) formulaArg {
	if argsList.Len() != 4 && argsList.Len() != 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "PRICEDISC requires 4 or 5 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	if maturity.Number <= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEDISC requires maturity > settlement")
	}
	discount := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if discount.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if discount.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEDISC requires discount > 0")
	}
	redemption := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if redemption.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if redemption.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEDISC requires redemption > 0")
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	frac := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	return newNumberFormulaArg(redemption.Number * (1 - discount.Number*frac.Number))
}

// PRICEMAT function calculates the price, per $100 face value of a security
// that pays interest at maturity. The syntax of the function is:
//
//	PRICEMAT(settlement,maturity,issue,rate,yld,[basis])
func (fn *formulaFuncs) PRICEMAT(argsList *list.List) formulaArg {
	if argsList.Len() != 5 && argsList.Len() != 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "PRICEMAT requires 5 or 6 arguments")
	}
	args := fn.prepareDataValueArgs(3, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity, issue := args.List[0], args.List[1], args.List[2]
	if settlement.Number >= maturity.Number {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEMAT requires maturity > settlement")
	}
	if issue.Number >= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEMAT requires settlement > issue")
	}
	rate := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEMAT requires rate >= 0")
	}
	yld := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if yld.Type != ArgNumber {
		return yld
	}
	if yld.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "PRICEMAT requires yld >= 0")
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 6 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	dsm := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if dsm.Type != ArgNumber {
		return dsm
	}
	dis := yearFrac(issue.Number, settlement.Number, int(basis.Number))
	dim := yearFrac(issue.Number, maturity.Number, int(basis.Number))
	return newNumberFormulaArg(((1+dim.Number*rate.Number)/(1+dsm.Number*yld.Number) - dis.Number*rate.Number) * 100)
}

// PV function calculates the Present Value of an investment, based on a
// series of future payments. The syntax of the function is:
//
//	PV(rate,nper,pmt,[fv],[type])
func (fn *formulaFuncs) PV(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "PV requires at least 3 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "PV allows at most 5 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	nper := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pmt := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pmt.Type != ArgNumber {
		return pmt
	}
	fv := newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if fv = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); fv.Type != ArgNumber {
			return fv
		}
	}
	t := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if t = argsList.Back().Value.(formulaArg).ToNumber(); t.Type != ArgNumber {
			return t
		}
		if t.Number != 0 {
			t.Number = 1
		}
	}
	if rate.Number == 0 {
		return newNumberFormulaArg(-pmt.Number*nper.Number - fv.Number)
	}
	return newNumberFormulaArg((((1-math.Pow(1+rate.Number, nper.Number))/rate.Number)*pmt.Number*(1+rate.Number*t.Number) - fv.Number) / math.Pow(1+rate.Number, nper.Number))
}

// rate is an implementation of the formula function RATE.
func (fn *formulaFuncs) rate(nper, pmt, pv, fv, t, guess formulaArg) formulaArg {
	maxIter, iter, isClose, epsMax, rate := 100, 0, false, 1e-6, guess.Number
	for iter < maxIter && !isClose {
		t1 := math.Pow(rate+1, nper.Number)
		t2 := math.Pow(rate+1, nper.Number-1)
		rt := rate*t.Number + 1
		p0 := pmt.Number * (t1 - 1)
		f1 := fv.Number + t1*pv.Number + p0*rt/rate
		n1 := nper.Number * t2 * pv.Number
		n2 := p0 * rt / math.Pow(rate, 2)
		f2 := math.Nextafter(n1, n1) - math.Nextafter(n2, n2)
		f3 := (nper.Number*pmt.Number*t2*rt + p0*t.Number) / rate
		delta := f1 / (f2 + f3)
		if math.Abs(delta) < epsMax {
			isClose = true
		}
		iter++
		rate -= delta
	}
	return newNumberFormulaArg(rate)
}

// RATE function calculates the interest rate required to pay off a specified
// amount of a loan, or to reach a target amount on an investment, over a
// given period. The syntax of the function is:
//
//	RATE(nper,pmt,pv,[fv],[type],[guess])
func (fn *formulaFuncs) RATE(argsList *list.List) formulaArg {
	if argsList.Len() < 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "RATE requires at least 3 arguments")
	}
	if argsList.Len() > 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "RATE allows at most 6 arguments")
	}
	nper := argsList.Front().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber {
		return nper
	}
	pmt := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if pmt.Type != ArgNumber {
		return pmt
	}
	pv := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pv.Type != ArgNumber {
		return pv
	}
	fv := newNumberFormulaArg(0)
	if argsList.Len() >= 4 {
		if fv = argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber(); fv.Type != ArgNumber {
			return fv
		}
	}
	t := newNumberFormulaArg(0)
	if argsList.Len() >= 5 {
		if t = argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); t.Type != ArgNumber {
			return t
		}
		if t.Number != 0 {
			t.Number = 1
		}
	}
	guess := newNumberFormulaArg(0.1)
	if argsList.Len() == 6 {
		if guess = argsList.Back().Value.(formulaArg).ToNumber(); guess.Type != ArgNumber {
			return guess
		}
	}
	return fn.rate(nper, pmt, pv, fv, t, guess)
}

// RECEIVED function calculates the amount received at maturity for a fully
// invested security. The syntax of the function is:
//
//	RECEIVED(settlement,maturity,investment,discount,[basis])
func (fn *formulaFuncs) RECEIVED(argsList *list.List) formulaArg {
	if argsList.Len() < 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "RECEIVED requires at least 4 arguments")
	}
	if argsList.Len() > 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "RECEIVED allows at most 5 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	investment := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if investment.Type != ArgNumber {
		return investment
	}
	discount := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if discount.Type != ArgNumber {
		return discount
	}
	if discount.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "RECEIVED requires discount > 0")
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	frac := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	return newNumberFormulaArg(investment.Number / (1 - discount.Number*frac.Number))
}

// RRI function calculates the equivalent interest rate for an investment with
// specified present value, future value and duration. The syntax of the
// function is:
//
//	RRI(nper,pv,fv)
func (fn *formulaFuncs) RRI(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "RRI requires 3 arguments")
	}
	nper := argsList.Front().Value.(formulaArg).ToNumber()
	pv := argsList.Front().Next().Value.(formulaArg).ToNumber()
	fv := argsList.Back().Value.(formulaArg).ToNumber()
	if nper.Type != ArgNumber || pv.Type != ArgNumber || fv.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if nper.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "RRI requires nper argument to be > 0")
	}
	if pv.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "RRI requires pv argument to be > 0")
	}
	if fv.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "RRI requires fv argument to be >= 0")
	}
	return newNumberFormulaArg(math.Pow(fv.Number/pv.Number, 1/nper.Number) - 1)
}

// SLN function calculates the straight line depreciation of an asset for one
// period. The syntax of the function is:
//
//	SLN(cost,salvage,life)
func (fn *formulaFuncs) SLN(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "SLN requires 3 arguments")
	}
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	salvage := argsList.Front().Next().Value.(formulaArg).ToNumber()
	life := argsList.Back().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber || salvage.Type != ArgNumber || life.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if life.Number == 0 {
		return newErrorFormulaArg(formulaErrorNUM, "SLN requires life argument to be > 0")
	}
	return newNumberFormulaArg((cost.Number - salvage.Number) / life.Number)
}

// SYD function calculates the sum-of-years' digits depreciation for a
// specified period in the lifetime of an asset. The syntax of the function
// is:
//
//	SYD(cost,salvage,life,per)
func (fn *formulaFuncs) SYD(argsList *list.List) formulaArg {
	if argsList.Len() != 4 {
		return newErrorFormulaArg(formulaErrorVALUE, "SYD requires 4 arguments")
	}
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	salvage := argsList.Front().Next().Value.(formulaArg).ToNumber()
	life := argsList.Back().Prev().Value.(formulaArg).ToNumber()
	per := argsList.Back().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber || salvage.Type != ArgNumber || life.Type != ArgNumber || per.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	if life.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "SYD requires life argument to be > 0")
	}
	if per.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "SYD requires per argument to be > 0")
	}
	if per.Number > life.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(((cost.Number - salvage.Number) * (life.Number - per.Number + 1) * 2) / (life.Number * (life.Number + 1)))
}

// TBILLEQ function calculates the bond-equivalent yield for a Treasury Bill.
// The syntax of the function is:
//
//	TBILLEQ(settlement,maturity,discount)
func (fn *formulaFuncs) TBILLEQ(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TBILLEQ requires 3 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	dsm := maturity.Number - settlement.Number
	if dsm > 365 || maturity.Number <= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	discount := argsList.Back().Value.(formulaArg).ToNumber()
	if discount.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if discount.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg((365 * discount.Number) / (360 - discount.Number*dsm))
}

// TBILLPRICE function returns the price, per $100 face value, of a Treasury
// Bill. The syntax of the function is:
//
//	TBILLPRICE(settlement,maturity,discount)
func (fn *formulaFuncs) TBILLPRICE(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TBILLPRICE requires 3 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	dsm := maturity.Number - settlement.Number
	if dsm > 365 || maturity.Number <= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	discount := argsList.Back().Value.(formulaArg).ToNumber()
	if discount.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if discount.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(100 * (1 - discount.Number*dsm/360))
}

// TBILLYIELD function calculates the yield of a Treasury Bill. The syntax of
// the function is:
//
//	TBILLYIELD(settlement,maturity,pr)
func (fn *formulaFuncs) TBILLYIELD(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "TBILLYIELD requires 3 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	dsm := maturity.Number - settlement.Number
	if dsm > 365 || maturity.Number <= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	pr := argsList.Back().Value.(formulaArg).ToNumber()
	if pr.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if pr.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(((100 - pr.Number) / pr.Number) * (360 / dsm))
}

// prepareVdbArgs checking and prepare arguments for the formula function
// VDB.
func (fn *formulaFuncs) prepareVdbArgs(argsList *list.List) formulaArg {
	cost := argsList.Front().Value.(formulaArg).ToNumber()
	if cost.Type != ArgNumber {
		return cost
	}
	if cost.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires cost >= 0")
	}
	salvage := argsList.Front().Next().Value.(formulaArg).ToNumber()
	if salvage.Type != ArgNumber {
		return salvage
	}
	if salvage.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires salvage >= 0")
	}
	life := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if life.Type != ArgNumber {
		return life
	}
	if life.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires life > 0")
	}
	startPeriod := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if startPeriod.Type != ArgNumber {
		return startPeriod
	}
	if startPeriod.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires start_period > 0")
	}
	endPeriod := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if endPeriod.Type != ArgNumber {
		return endPeriod
	}
	if startPeriod.Number > endPeriod.Number {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires start_period <= end_period")
	}
	if endPeriod.Number > life.Number {
		return newErrorFormulaArg(formulaErrorNUM, "VDB requires end_period <= life")
	}
	factor := newNumberFormulaArg(2)
	if argsList.Len() > 5 {
		if factor = argsList.Front().Next().Next().Next().Next().Next().Value.(formulaArg).ToNumber(); factor.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		if factor.Number < 0 {
			return newErrorFormulaArg(formulaErrorVALUE, "VDB requires factor >= 0")
		}
	}
	return newListFormulaArg([]formulaArg{cost, salvage, life, startPeriod, endPeriod, factor})
}

// vdb is a part of implementation of the formula function VDB.
func (fn *formulaFuncs) vdb(cost, salvage, life, life1, period, factor formulaArg) formulaArg {
	var ddb, vdb, sln, term float64
	endInt, cs, nowSln := math.Ceil(period.Number), cost.Number-salvage.Number, false
	ddbArgs := list.New()
	for i := 1.0; i <= endInt; i++ {
		if !nowSln {
			ddbArgs.Init()
			ddbArgs.PushBack(cost)
			ddbArgs.PushBack(salvage)
			ddbArgs.PushBack(life)
			ddbArgs.PushBack(newNumberFormulaArg(i))
			ddbArgs.PushBack(factor)
			ddb = fn.DDB(ddbArgs).Number
			sln = cs / (life1.Number - i + 1)
			if sln > ddb {
				term = sln
				nowSln = true
			} else {
				term = ddb
				cs -= ddb
			}
		} else {
			term = sln
		}
		if i == endInt {
			term *= period.Number + 1 - endInt
		}
		vdb += term
	}
	return newNumberFormulaArg(vdb)
}

// VDB function calculates the depreciation of an asset, using the Double
// Declining Balance Method, or another specified depreciation rate, for a
// specified period (including partial periods). The syntax of the function
// is:
//
//	VDB(cost,salvage,life,start_period,end_period,[factor],[no_switch])
func (fn *formulaFuncs) VDB(argsList *list.List) formulaArg {
	if argsList.Len() < 5 || argsList.Len() > 7 {
		return newErrorFormulaArg(formulaErrorVALUE, "VDB requires 5 or 7 arguments")
	}
	args := fn.prepareVdbArgs(argsList)
	if args.Type != ArgList {
		return args
	}
	cost, salvage, life, startPeriod, endPeriod, factor := args.List[0], args.List[1], args.List[2], args.List[3], args.List[4], args.List[5]
	noSwitch := newBoolFormulaArg(false)
	if argsList.Len() > 6 {
		if noSwitch = argsList.Back().Value.(formulaArg).ToBool(); noSwitch.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	startInt, endInt, vdb, ddbArgs := math.Floor(startPeriod.Number), math.Ceil(endPeriod.Number), newNumberFormulaArg(0), list.New()
	if noSwitch.Number == 1 {
		for i := startInt + 1; i <= endInt; i++ {
			ddbArgs.Init()
			ddbArgs.PushBack(cost)
			ddbArgs.PushBack(salvage)
			ddbArgs.PushBack(life)
			ddbArgs.PushBack(newNumberFormulaArg(i))
			ddbArgs.PushBack(factor)
			term := fn.DDB(ddbArgs)
			if i == startInt+1 {
				term.Number *= math.Min(endPeriod.Number, startInt+1) - startPeriod.Number
			} else if i == endInt {
				term.Number *= endPeriod.Number + 1 - endInt
			}
			vdb.Number += term.Number
		}
		return vdb
	}
	life1, part := life, 0.0
	if startPeriod.Number != math.Floor(startPeriod.Number) && factor.Number > 1.0 && startPeriod.Number >= life.Number/2.0 {
		part = startPeriod.Number - life.Number/2.0
		startPeriod.Number = life.Number / 2.0
		endPeriod.Number -= part
	}
	cost.Number -= fn.vdb(cost, salvage, life, life1, startPeriod, factor).Number
	return fn.vdb(cost, salvage, life, newNumberFormulaArg(life.Number-startPeriod.Number), newNumberFormulaArg(endPeriod.Number-startPeriod.Number), factor)
}

// prepareXArgs prepare arguments for the formula function XIRR and XNPV.
func (fn *formulaFuncs) prepareXArgs(values, dates formulaArg) (valuesArg, datesArg []float64, err formulaArg) {
	for _, arg := range values.ToList() {
		if numArg := arg.ToNumber(); numArg.Type == ArgNumber {
			valuesArg = append(valuesArg, numArg.Number)
			continue
		}
		err = newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		return
	}
	if len(valuesArg) < 2 {
		err = newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		return
	}
	args, date := list.New(), 0.0
	for _, arg := range dates.ToList() {
		args.Init()
		args.PushBack(arg)
		dateValue := fn.DATEVALUE(args)
		if dateValue.Type != ArgNumber {
			err = newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
			return
		}
		if dateValue.Number < date {
			err = newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
			return
		}
		datesArg = append(datesArg, dateValue.Number)
		date = dateValue.Number
	}
	if len(valuesArg) != len(datesArg) {
		err = newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		return
	}
	err = newEmptyFormulaArg()
	return
}

// xirr is an implementation of the formula function XIRR.
func (fn *formulaFuncs) xirr(values, dates []float64, guess float64) formulaArg {
	positive, negative := false, false
	for i := 0; i < len(values); i++ {
		if values[i] > 0 {
			positive = true
		}
		if values[i] < 0 {
			negative = true
		}
	}
	if !positive || !negative {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	result, epsMax, count, maxIterate, err := guess, 1e-10, 0, 50, false
	for {
		resultValue := xirrPart1(values, dates, result)
		newRate := result - resultValue/xirrPart2(values, dates, result)
		epsRate := math.Abs(newRate - result)
		result = newRate
		count++
		if epsRate <= epsMax || math.Abs(resultValue) <= epsMax {
			break
		}
		if count > maxIterate {
			err = true
			break
		}
	}
	if err || math.IsNaN(result) || math.IsInf(result, 0) {
		return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
	}
	return newNumberFormulaArg(result)
}

// xirrPart1 is a part of implementation of the formula function XIRR.
func xirrPart1(values, dates []float64, rate float64) float64 {
	r := rate + 1
	result := values[0]
	vlen := len(values)
	firstDate := dates[0]
	for i := 1; i < vlen; i++ {
		result += values[i] / math.Pow(r, (dates[i]-firstDate)/365)
	}
	return result
}

// xirrPart2 is a part of implementation of the formula function XIRR.
func xirrPart2(values, dates []float64, rate float64) float64 {
	r := rate + 1
	result := 0.0
	vlen := len(values)
	firstDate := dates[0]
	for i := 1; i < vlen; i++ {
		frac := (dates[i] - firstDate) / 365
		result -= frac * values[i] / math.Pow(r, frac+1)
	}
	return result
}

// XIRR function returns the Internal Rate of Return for a supplied series of
// cash flows (i.e. a set of values, which includes an initial investment
// value and a series of net income values) occurring at a series of supplied
// dates. The syntax of the function is:
//
//	XIRR(values,dates,[guess])
func (fn *formulaFuncs) XIRR(argsList *list.List) formulaArg {
	if argsList.Len() != 2 && argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "XIRR requires 2 or 3 arguments")
	}
	values, dates, err := fn.prepareXArgs(argsList.Front().Value.(formulaArg), argsList.Front().Next().Value.(formulaArg))
	if err.Type != ArgEmpty {
		return err
	}
	guess := newNumberFormulaArg(0)
	if argsList.Len() == 3 {
		if guess = argsList.Back().Value.(formulaArg).ToNumber(); guess.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
		if guess.Number <= -1 {
			return newErrorFormulaArg(formulaErrorVALUE, "XIRR requires guess > -1")
		}
	}
	return fn.xirr(values, dates, guess.Number)
}

// XNPV function calculates the Net Present Value for a schedule of cash flows
// that is not necessarily periodic. The syntax of the function is:
//
//	XNPV(rate,values,dates)
func (fn *formulaFuncs) XNPV(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "XNPV requires 3 arguments")
	}
	rate := argsList.Front().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number <= 0 {
		return newErrorFormulaArg(formulaErrorVALUE, "XNPV requires rate > 0")
	}
	values, dates, err := fn.prepareXArgs(argsList.Front().Next().Value.(formulaArg), argsList.Back().Value.(formulaArg))
	if err.Type != ArgEmpty {
		return err
	}
	date1, xnpv := dates[0], 0.0
	for idx, value := range values {
		xnpv += value / math.Pow(1+rate.Number, (dates[idx]-date1)/365)
	}
	return newNumberFormulaArg(xnpv)
}

// yield is an implementation of the formula function YIELD.
func (fn *formulaFuncs) yield(settlement, maturity, rate, pr, redemption, frequency, basis formulaArg) formulaArg {
	priceN, yield1, yield2 := newNumberFormulaArg(0), newNumberFormulaArg(0), newNumberFormulaArg(1)
	price1 := fn.price(settlement, maturity, rate, yield1, redemption, frequency, basis)
	if price1.Type != ArgNumber {
		return price1
	}
	price2 := fn.price(settlement, maturity, rate, yield2, redemption, frequency, basis)
	yieldN := newNumberFormulaArg((yield2.Number - yield1.Number) * 0.5)
	for iter := 0; iter < 100 && priceN.Number != pr.Number; iter++ {
		priceN = fn.price(settlement, maturity, rate, yieldN, redemption, frequency, basis)
		if pr.Number == price1.Number {
			return yield1
		} else if pr.Number == price2.Number {
			return yield2
		} else if pr.Number == priceN.Number {
			return yieldN
		} else if pr.Number < price2.Number {
			yield2.Number *= 2.0
			price2 = fn.price(settlement, maturity, rate, yield2, redemption, frequency, basis)
			yieldN.Number = (yield2.Number - yield1.Number) * 0.5
		} else {
			if pr.Number < priceN.Number {
				yield1 = yieldN
				price1 = priceN
			} else {
				yield2 = yieldN
				price2 = priceN
			}
			f1 := (yield2.Number - yield1.Number) * ((pr.Number - price2.Number) / (price1.Number - price2.Number))
			yieldN.Number = yield2.Number - math.Nextafter(f1, f1)
		}
	}
	return yieldN
}

// YIELD function calculates the Yield of a security that pays periodic
// interest. The syntax of the function is:
//
//	YIELD(settlement,maturity,rate,pr,redemption,frequency,[basis])
func (fn *formulaFuncs) YIELD(argsList *list.List) formulaArg {
	return fn.priceYield("YIELD", argsList)
}

// YIELDDISC function calculates the annual yield of a discounted security.
// The syntax of the function is:
//
//	YIELDDISC(settlement,maturity,pr,redemption,[basis])
func (fn *formulaFuncs) YIELDDISC(argsList *list.List) formulaArg {
	if argsList.Len() != 4 && argsList.Len() != 5 {
		return newErrorFormulaArg(formulaErrorVALUE, "YIELDDISC requires 4 or 5 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	pr := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if pr.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if pr.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "YIELDDISC requires pr > 0")
	}
	redemption := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if redemption.Type != ArgNumber {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	if redemption.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "YIELDDISC requires redemption > 0")
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 5 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	frac := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	if frac.Type != ArgNumber {
		return frac
	}
	return newNumberFormulaArg((redemption.Number/pr.Number - 1) / frac.Number)
}

// YIELDMAT function calculates the annual yield of a security that pays
// interest at maturity. The syntax of the function is:
//
//	YIELDMAT(settlement,maturity,issue,rate,pr,[basis])
func (fn *formulaFuncs) YIELDMAT(argsList *list.List) formulaArg {
	if argsList.Len() != 5 && argsList.Len() != 6 {
		return newErrorFormulaArg(formulaErrorVALUE, "YIELDMAT requires 5 or 6 arguments")
	}
	args := fn.prepareDataValueArgs(2, argsList)
	if args.Type != ArgList {
		return args
	}
	settlement, maturity := args.List[0], args.List[1]
	arg := list.New().Init()
	issue := argsList.Front().Next().Next().Value.(formulaArg).ToNumber()
	if issue.Type != ArgNumber {
		arg.PushBack(argsList.Front().Next().Next().Value.(formulaArg))
		issue = fn.DATEVALUE(arg)
		if issue.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
		}
	}
	if issue.Number >= settlement.Number {
		return newErrorFormulaArg(formulaErrorNUM, "YIELDMAT requires settlement > issue")
	}
	rate := argsList.Front().Next().Next().Next().Value.(formulaArg).ToNumber()
	if rate.Type != ArgNumber {
		return rate
	}
	if rate.Number < 0 {
		return newErrorFormulaArg(formulaErrorNUM, "YIELDMAT requires rate >= 0")
	}
	pr := argsList.Front().Next().Next().Next().Next().Value.(formulaArg).ToNumber()
	if pr.Type != ArgNumber {
		return pr
	}
	if pr.Number <= 0 {
		return newErrorFormulaArg(formulaErrorNUM, "YIELDMAT requires pr > 0")
	}
	basis := newNumberFormulaArg(0)
	if argsList.Len() == 6 {
		if basis = argsList.Back().Value.(formulaArg).ToNumber(); basis.Type != ArgNumber {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	dim := yearFrac(issue.Number, maturity.Number, int(basis.Number))
	if dim.Type != ArgNumber {
		return dim
	}
	dis := yearFrac(issue.Number, settlement.Number, int(basis.Number))
	dsm := yearFrac(settlement.Number, maturity.Number, int(basis.Number))
	f1 := dim.Number * rate.Number
	result := 1 + math.Nextafter(f1, f1)
	result /= pr.Number/100 + dis.Number*rate.Number
	result--
	result /= dsm.Number
	return newNumberFormulaArg(result)
}

// Database Functions

// calcDatabase defines the structure for formula database.
type calcDatabase struct {
	col, row int
	indexMap map[int]int
	database [][]formulaArg
	criteria [][]formulaArg
}

// newCalcDatabase function returns formula database by given data range of
// cells containing the database, field and criteria range.
func newCalcDatabase(database, field, criteria formulaArg) *calcDatabase {
	db := calcDatabase{
		indexMap: make(map[int]int),
		database: database.Matrix,
		criteria: criteria.Matrix,
	}
	exp := len(database.Matrix) < 2 || len(database.Matrix[0]) < 1 ||
		len(criteria.Matrix) < 2 || len(criteria.Matrix[0]) < 1
	if field.Type != ArgEmpty {
		if db.col = db.columnIndex(database.Matrix, field); exp || db.col < 0 || len(db.database[0]) <= db.col {
			return nil
		}
		return &db
	}
	if db.col = -1; exp {
		return nil
	}
	return &db
}

// columnIndex return index by specifies column field within the database for
// which user want to return the count of non-blank cells.
func (db *calcDatabase) columnIndex(database [][]formulaArg, field formulaArg) int {
	num := field.ToNumber()
	if num.Type != ArgNumber && len(database) > 0 {
		for i := 0; i < len(database[0]); i++ {
			if title := database[0][i]; strings.EqualFold(title.Value(), field.Value()) {
				return i
			}
		}
		return -1
	}
	return int(num.Number - 1)
}

// criteriaEval evaluate formula criteria expression.
func (db *calcDatabase) criteriaEval() bool {
	var (
		columns, rows = len(db.criteria[0]), len(db.criteria)
		criteria      = db.criteria
		k             int
		matched       bool
	)
	if len(db.indexMap) == 0 {
		fields := criteria[0]
		for j := 0; j < columns; j++ {
			if k = db.columnIndex(db.database, fields[j]); k < 0 {
				return false
			}
			db.indexMap[j] = k
		}
	}
	for i := 1; !matched && i < rows; i++ {
		matched = true
		for j := 0; matched && j < columns; j++ {
			criteriaExp := db.criteria[i][j]
			if criteriaExp.Value() == "" {
				continue
			}
			criteria := formulaCriteriaParser(criteriaExp)
			cell := db.database[db.row][db.indexMap[j]]
			matched, _ = formulaCriteriaEval(cell, criteria)
		}
	}
	return matched
}

// value returns the current cell value.
func (db *calcDatabase) value() formulaArg {
	if db.col == -1 {
		return db.database[db.row][len(db.database[db.row])-1]
	}
	return db.database[db.row][db.col]
}

// next will return true if find the matched cell in the database.
func (db *calcDatabase) next() bool {
	matched, rows := false, len(db.database)
	for !matched && db.row < rows {
		if db.row++; db.row < rows {
			matched = db.criteriaEval()
		}
	}
	return matched
}

// database is an implementation of the formula functions DAVERAGE, DMAX and DMIN.
func (fn *formulaFuncs) database(name string, argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires 3 arguments", name))
	}
	database := argsList.Front().Value.(formulaArg)
	field := argsList.Front().Next().Value.(formulaArg)
	criteria := argsList.Back().Value.(formulaArg)
	db := newCalcDatabase(database, field, criteria)
	if db == nil {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	args := list.New()
	for db.next() {
		args.PushBack(db.value())
	}
	switch name {
	case "DMAX":
		return fn.MAX(args)
	case "DMIN":
		return fn.MIN(args)
	case "DPRODUCT":
		return fn.PRODUCT(args)
	case "DSTDEV":
		return fn.STDEV(args)
	case "DSTDEVP":
		return fn.STDEVP(args)
	case "DSUM":
		return fn.SUM(args)
	case "DVAR":
		return fn.VAR(args)
	case "DVARP":
		return fn.VARP(args)
	default:
		return fn.AVERAGE(args)
	}
}

// DAVERAGE function calculates the average (statistical mean) of values in a
// field (column) in a database for selected records, that satisfy
// user-specified criteria. The syntax of the function is:
//
//	DAVERAGE(database,field,criteria)
func (fn *formulaFuncs) DAVERAGE(argsList *list.List) formulaArg {
	return fn.database("DAVERAGE", argsList)
}

// dcount is an implementation of the formula functions DCOUNT and DCOUNTA.
func (fn *formulaFuncs) dcount(name string, argsList *list.List) formulaArg {
	if argsList.Len() < 2 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s requires at least 2 arguments", name))
	}
	if argsList.Len() > 3 {
		return newErrorFormulaArg(formulaErrorVALUE, fmt.Sprintf("%s allows at most 3 arguments", name))
	}
	field := newEmptyFormulaArg()
	criteria := argsList.Back().Value.(formulaArg)
	if argsList.Len() > 2 {
		field = argsList.Front().Next().Value.(formulaArg)
	}
	database := argsList.Front().Value.(formulaArg)
	db := newCalcDatabase(database, field, criteria)
	if db == nil {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	args := list.New()
	for db.next() {
		args.PushBack(db.value())
	}
	if name == "DCOUNT" {
		return fn.COUNT(args)
	}
	return fn.COUNTA(args)
}

// DCOUNT function returns the number of cells containing numeric values, in a
// field (column) of a database for selected records only. The records to be
// included in the count are those that satisfy a set of one or more
// user-specified criteria. The syntax of the function is:
//
//	DCOUNT(database,[field],criteria)
func (fn *formulaFuncs) DCOUNT(argsList *list.List) formulaArg {
	return fn.dcount("DCOUNT", argsList)
}

// DCOUNTA function returns the number of non-blank cells, in a field
// (column) of a database for selected records only. The records to be
// included in the count are those that satisfy a set of one or more
// user-specified criteria. The syntax of the function is:
//
//	DCOUNTA(database,[field],criteria)
func (fn *formulaFuncs) DCOUNTA(argsList *list.List) formulaArg {
	return fn.dcount("DCOUNTA", argsList)
}

// DGET function returns a single value from a column of a database. The record
// is selected via a set of one or more user-specified criteria. The syntax of
// the function is:
//
//	DGET(database,field,criteria)
func (fn *formulaFuncs) DGET(argsList *list.List) formulaArg {
	if argsList.Len() != 3 {
		return newErrorFormulaArg(formulaErrorVALUE, "DGET requires 3 arguments")
	}
	database := argsList.Front().Value.(formulaArg)
	field := argsList.Front().Next().Value.(formulaArg)
	criteria := argsList.Back().Value.(formulaArg)
	db := newCalcDatabase(database, field, criteria)
	if db == nil {
		return newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	}
	value := newErrorFormulaArg(formulaErrorVALUE, formulaErrorVALUE)
	if db.next() {
		if value = db.value(); db.next() {
			return newErrorFormulaArg(formulaErrorNUM, formulaErrorNUM)
		}
	}
	return value
}

// DMAX function finds the maximum value in a field (column) in a database for
// selected records only. The records to be included in the calculation are
// defined by a set of one or more user-specified criteria. The syntax of the
// function is:
//
//	DMAX(database,field,criteria)
func (fn *formulaFuncs) DMAX(argsList *list.List) formulaArg {
	return fn.database("DMAX", argsList)
}

// DMIN function finds the minimum value in a field (column) in a database for
// selected records only. The records to be included in the calculation are
// defined by a set of one or more user-specified criteria. The syntax of the
// function is:
//
//	DMIN(database,field,criteria)
func (fn *formulaFuncs) DMIN(argsList *list.List) formulaArg {
	return fn.database("DMIN", argsList)
}

// DPRODUCT function calculates the product of a field (column) in a database
// for selected records, that satisfy user-specified criteria. The syntax of
// the function is:
//
//	DPRODUCT(database,field,criteria)
func (fn *formulaFuncs) DPRODUCT(argsList *list.List) formulaArg {
	return fn.database("DPRODUCT", argsList)
}

// DSTDEV function calculates the sample standard deviation of a field
// (column) in a database for selected records only. The records to be
// included in the calculation are defined by a set of one or more
// user-specified criteria. The syntax of the function is:
//
//	DSTDEV(database,field,criteria)
func (fn *formulaFuncs) DSTDEV(argsList *list.List) formulaArg {
	return fn.database("DSTDEV", argsList)
}

// DSTDEVP function calculates the standard deviation of a field (column) in a
// database for selected records only. The records to be included in the
// calculation are defined by a set of one or more user-specified criteria.
// The syntax of the function is:
//
//	DSTDEVP(database,field,criteria)
func (fn *formulaFuncs) DSTDEVP(argsList *list.List) formulaArg {
	return fn.database("DSTDEVP", argsList)
}

// DSUM function calculates the sum of a field (column) in a database for
// selected records, that satisfy user-specified criteria. The syntax of the
// function is:
//
//	DSUM(database,field,criteria)
func (fn *formulaFuncs) DSUM(argsList *list.List) formulaArg {
	return fn.database("DSUM", argsList)
}

// DVAR function calculates the sample variance of a field (column) in a
// database for selected records only. The records to be included in the
// calculation are defined by a set of one or more user-specified criteria.
// The syntax of the function is:
//
//	DVAR(database,field,criteria)
func (fn *formulaFuncs) DVAR(argsList *list.List) formulaArg {
	return fn.database("DVAR", argsList)
}

// DVARP function calculates the variance (for an entire population), of the
// values in a field (column) in a database for selected records only. The
// records to be included in the calculation are defined by a set of one or
// more user-specified criteria. The syntax of the function is:
//
//	DVARP(database,field,criteria)
func (fn *formulaFuncs) DVARP(argsList *list.List) formulaArg {
	return fn.database("DVARP", argsList)
}