

package excelize

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// CellType is the type of cell value type.
type CellType byte

// Cell value types enumeration.
const (
	CellTypeUnset CellType = iota
	CellTypeBool
	CellTypeDate
	CellTypeError
	CellTypeFormula
	CellTypeInlineString
	CellTypeNumber
	CellTypeSharedString
)

const (
	// STCellFormulaTypeArray defined the formula is an array formula.
	STCellFormulaTypeArray = "array"
	// STCellFormulaTypeDataTable defined the formula is a data table formula.
	STCellFormulaTypeDataTable = "dataTable"
	// STCellFormulaTypeNormal defined the formula is a regular cell formula.
	STCellFormulaTypeNormal = "normal"
	// STCellFormulaTypeShared defined the formula is part of a shared formula.
	STCellFormulaTypeShared = "shared"
)

// cellTypes mapping the cell's data type and enumeration.
var cellTypes = map[string]CellType{
	"b":         CellTypeBool,
	"d":         CellTypeDate,
	"n":         CellTypeNumber,
	"e":         CellTypeError,
	"s":         CellTypeSharedString,
	"str":       CellTypeFormula,
	"inlineStr": CellTypeInlineString,
}

// GetCellValue provides a function to get formatted value from cell by given
// worksheet name and cell reference in spreadsheet. The return value is
// converted to the 'string' data type. This function is concurrency safe. If
// the cell format can be applied to the value of a cell, the applied value
// will be returned, otherwise the original value will be returned. All cells'
// values will be the same in a merged range.
func (f *File) GetCellValue(sheet, cell string, opts ...Options) (string, error) {
	return f.getCellStringFunc(sheet, cell, func(x *xlsxWorksheet, c *xlsxC) (string, bool, error) {
		sst, err := f.sharedStringsReader()
		if err != nil {
			return "", true, err
		}
		val, err := c.getValueFrom(f, sst, getOptions(opts...).RawCellValue)
		return val, true, err
	})
}

// GetCellType provides a function to get the cell's data type by given
// worksheet name and cell reference in spreadsheet file.
func (f *File) GetCellType(sheet, cell string) (CellType, error) {
	var (
		err         error
		cellTypeStr string
		cellType    CellType
	)
	if cellTypeStr, err = f.getCellStringFunc(sheet, cell, func(x *xlsxWorksheet, c *xlsxC) (string, bool, error) {
		return c.T, true, nil
	}); err != nil {
		return CellTypeUnset, err
	}
	cellType = cellTypes[cellTypeStr]
	return cellType, err
}

// SetCellValue provides a function to set the value of a cell. This function
// is concurrency safe. The specified coordinates should not be in the first
// row of the table, a complex number can be set with string text. The
// following shows the supported data types:
//
//	int
//	int8
//	int16
//	int32
//	int64
//	uint
//	uint8
//	uint16
//	uint32
//	uint64
//	float32
//	float64
//	string
//	[]byte
//	time.Duration
//	time.Time
//	bool
//	nil
//
// Note that default date format is m/d/yy h:mm of time.Time type value. You
// can set numbers format by the SetCellStyle function. If you need to set the
// specialized date in Excel like January 0, 1900 or February 29, 1900, these
// times can not representation in Go language time.Time data type. Please set
// the cell value as number 0 or 60, then create and bind the date-time number
// format style for the cell.
func (f *File) SetCellValue(sheet, cell string, value interface{}) error {
	var err error
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		err = f.setCellIntFunc(sheet, cell, v)
	case float32:
		err = f.SetCellFloat(sheet, cell, float64(v), -1, 32)
	case float64:
		err = f.SetCellFloat(sheet, cell, v, -1, 64)
	case string:
		err = f.SetCellStr(sheet, cell, v)
	case []byte:
		err = f.SetCellStr(sheet, cell, string(v))
	case time.Duration:
		_, d := setCellDuration(v)
		err = f.SetCellDefault(sheet, cell, d)
		if err != nil {
			return err
		}
		err = f.setDefaultTimeStyle(sheet, cell, 21)
	case time.Time:
		err = f.setCellTimeFunc(sheet, cell, v)
	case bool:
		err = f.SetCellBool(sheet, cell, v)
	case nil:
		err = f.SetCellDefault(sheet, cell, "")
	default:
		err = f.SetCellStr(sheet, cell, fmt.Sprint(value))
	}
	return err
}

// String extracts characters from a string item.
func (x xlsxSI) String() string {
	var value strings.Builder
	if x.T != nil {
		value.WriteString(x.T.Val)
	}
	for _, s := range x.R {
		if s.T != nil {
			value.WriteString(s.T.Val)
		}
	}
	if value.Len() == 0 {
		return ""
	}
	return bstrUnmarshal(value.String())
}

// hasValue determine if cell non-blank value.
func (c *xlsxC) hasValue() bool {
	return c.S != 0 || c.V != "" || c.F != nil || c.T != ""
}

// removeFormula delete formula for the cell.
func (f *File) removeFormula(c *xlsxC, ws *xlsxWorksheet, sheet string) error {
	if c.F != nil && c.Vm == nil {
		sheetID := f.getSheetID(sheet)
		if err := f.deleteCalcChain(sheetID, c.R); err != nil {
			return err
		}
		if c.F.T == STCellFormulaTypeShared && c.F.Ref != "" {
			si := c.F.Si
			for r, row := range ws.SheetData.Row {
				for col, cell := range row.C {
					if cell.F != nil && cell.F.Si != nil && *cell.F.Si == *si {
						ws.SheetData.Row[r].C[col].F = nil
						_ = f.deleteCalcChain(sheetID, cell.R)
					}
				}
			}
		}
		c.F = nil
	}
	return nil
}

// setCellIntFunc is a wrapper of SetCellInt.
func (f *File) setCellIntFunc(sheet, cell string, value interface{}) error {
	var err error
	switch v := value.(type) {
	case int:
		err = f.SetCellInt(sheet, cell, v)
	case int8:
		err = f.SetCellInt(sheet, cell, int(v))
	case int16:
		err = f.SetCellInt(sheet, cell, int(v))
	case int32:
		err = f.SetCellInt(sheet, cell, int(v))
	case int64:
		err = f.SetCellInt(sheet, cell, int(v))
	case uint:
		err = f.SetCellUint(sheet, cell, uint64(v))
	case uint8:
		err = f.SetCellUint(sheet, cell, uint64(v))
	case uint16:
		err = f.SetCellUint(sheet, cell, uint64(v))
	case uint32:
		err = f.SetCellUint(sheet, cell, uint64(v))
	case uint64:
		err = f.SetCellUint(sheet, cell, v)
	}
	return err
}

// setCellTimeFunc provides a method to process time type of value for
// SetCellValue.
func (f *File) setCellTimeFunc(sheet, cell string, value time.Time) error {
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return err
	}
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	ws.mu.Lock()
	c.S = ws.prepareCellStyle(col, row, c.S)
	ws.mu.Unlock()
	var date1904, isNum bool
	wb, err := f.workbookReader()
	if err != nil {
		return err
	}
	if wb != nil && wb.WorkbookPr != nil {
		date1904 = wb.WorkbookPr.Date1904
	}
	if isNum, err = c.setCellTime(value, date1904); err != nil {
		return err
	}
	if isNum {
		_ = f.setDefaultTimeStyle(sheet, cell, 22)
	}
	return err
}

// setCellTime prepares cell type and Excel time by given Go time.Time type
// timestamp.
func (c *xlsxC) setCellTime(value time.Time, date1904 bool) (isNum bool, err error) {
	var excelTime float64
	_, offset := value.In(value.Location()).Zone()
	value = value.Add(time.Duration(offset) * time.Second)
	if excelTime, err = timeToExcelTime(value, date1904); err != nil {
		return
	}
	isNum = excelTime > 0
	if isNum {
		c.setCellDefault(strconv.FormatFloat(excelTime, 'f', -1, 64))
	} else {
		c.setCellDefault(value.Format(time.RFC3339Nano))
	}
	return
}

// setCellDuration prepares cell type and value by given Go time.Duration type
// time duration.
func setCellDuration(value time.Duration) (t string, v string) {
	v = strconv.FormatFloat(value.Seconds()/86400, 'f', -1, 32)
	return
}

// SetCellInt provides a function to set int type value of a cell by given
// worksheet name, cell reference and cell value.
func (f *File) SetCellInt(sheet, cell string, value int) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	c.T, c.V = setCellInt(value)
	c.IS = nil
	return f.removeFormula(c, ws, sheet)
}

// setCellInt prepares cell type and string type cell value by a given integer.
func setCellInt(value int) (t string, v string) {
	v = strconv.Itoa(value)
	return
}

// SetCellUint provides a function to set uint type value of a cell by given
// worksheet name, cell reference and cell value.
func (f *File) SetCellUint(sheet, cell string, value uint64) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	c.T, c.V = setCellUint(value)
	c.IS = nil
	return f.removeFormula(c, ws, sheet)
}

// setCellUint prepares cell type and string type cell value by a given unsigned
// integer.
func setCellUint(value uint64) (t string, v string) {
	v = strconv.FormatUint(value, 10)
	return
}

// SetCellBool provides a function to set bool type value of a cell by given
// worksheet name, cell reference and cell value.
func (f *File) SetCellBool(sheet, cell string, value bool) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	c.T, c.V = setCellBool(value)
	c.IS = nil
	return f.removeFormula(c, ws, sheet)
}

// setCellBool prepares cell type and string type cell value by a given boolean
// value.
func setCellBool(value bool) (t string, v string) {
	t = "b"
	if value {
		v = "1"
	} else {
		v = "0"
	}
	return
}

// SetCellFloat sets a floating point value into a cell. The precision
// parameter specifies how many places after the decimal will be shown
// while -1 is a special value that will use as many decimal places as
// necessary to represent the number. bitSize is 32 or 64 depending on if a
// float32 or float64 was originally used for the value. For Example:
//
//	var x float32 = 1.325
//	f.SetCellFloat("Sheet1", "A1", float64(x), 2, 32)
func (f *File) SetCellFloat(sheet, cell string, value float64, precision, bitSize int) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	c.T, c.V = setCellFloat(value, precision, bitSize)
	c.IS = nil
	return f.removeFormula(c, ws, sheet)
}

// setCellFloat prepares cell type and string type cell value by a given float
// value.
func setCellFloat(value float64, precision, bitSize int) (t string, v string) {
	v = strconv.FormatFloat(value, 'f', precision, bitSize)
	return
}

// SetCellStr provides a function to set string type value of a cell. Total
// number of characters that a cell can contain 32767 characters.
func (f *File) SetCellStr(sheet, cell, value string) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	if c.T, c.V, err = f.setCellString(value); err != nil {
		return err
	}
	c.IS = nil
	return f.removeFormula(c, ws, sheet)
}

// setCellString provides a function to set string type to shared string table.
func (f *File) setCellString(value string) (t, v string, err error) {
	if utf8.RuneCountInString(value) > TotalCellChars {
		value = string([]rune(value)[:TotalCellChars])
	}
	t = "s"
	var si int
	if si, err = f.setSharedString(value); err != nil {
		return
	}
	v = strconv.Itoa(si)
	return
}

// sharedStringsLoader load shared string table from system temporary file to
// memory, and reset shared string table for reader.
func (f *File) sharedStringsLoader() (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if path, ok := f.tempFiles.Load(defaultXMLPathSharedStrings); ok {
		f.Pkg.Store(defaultXMLPathSharedStrings, f.readBytes(defaultXMLPathSharedStrings))
		f.tempFiles.Delete(defaultXMLPathSharedStrings)
		if err = os.Remove(path.(string)); err != nil {
			return
		}
		f.SharedStrings = nil
	}
	if f.sharedStringTemp != nil {
		if err := f.sharedStringTemp.Close(); err != nil {
			return err
		}
		f.tempFiles.Delete(defaultTempFileSST)
		f.sharedStringItem, err = nil, os.Remove(f.sharedStringTemp.Name())
		f.sharedStringTemp = nil
	}
	return
}

// setSharedString provides a function to add string to the share string table.
func (f *File) setSharedString(val string) (int, error) {
	if err := f.sharedStringsLoader(); err != nil {
		return 0, err
	}
	sst, err := f.sharedStringsReader()
	if err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.sharedStringsMap[val]; ok {
		return i, nil
	}
	sst.mu.Lock()
	defer sst.mu.Unlock()
	sst.Count++
	sst.UniqueCount++
	t := xlsxT{Val: val}
	val, t.Space = trimCellValue(val, false)
	sst.SI = append(sst.SI, xlsxSI{T: &t})
	f.sharedStringsMap[val] = sst.UniqueCount - 1
	return sst.UniqueCount - 1, nil
}

// trimCellValue provides a function to set string type to cell.
func trimCellValue(value string, escape bool) (v string, ns xml.Attr) {
	if utf8.RuneCountInString(value) > TotalCellChars {
		value = string([]rune(value)[:TotalCellChars])
	}
	if escape {
		var buf bytes.Buffer
		_ = xml.EscapeText(&buf, []byte(value))
		value = buf.String()
	}
	if len(value) > 0 {
		prefix, suffix := value[0], value[len(value)-1]
		for _, ascii := range []byte{9, 10, 13, 32} {
			if prefix == ascii || suffix == ascii {
				ns = xml.Attr{
					Name:  xml.Name{Space: NameSpaceXML, Local: "space"},
					Value: "preserve",
				}
				break
			}
		}
	}
	v = bstrMarshal(value)
	return
}

// setCellValue set cell data type and value for (inline) rich string cell or
// formula cell.
func (c *xlsxC) setCellValue(val string) {
	if c.F != nil {
		c.setStr(val)
		return
	}
	c.setInlineStr(val)
}

// setInlineStr set cell data type and value which containing an (inline) rich
// string.
func (c *xlsxC) setInlineStr(val string) {
	c.T, c.V, c.IS = "inlineStr", "", &xlsxSI{T: &xlsxT{}}
	c.IS.T.Val, c.IS.T.Space = trimCellValue(val, true)
}

// setStr set cell data type and value which containing a formula string.
func (c *xlsxC) setStr(val string) {
	c.T, c.IS = "str", nil
	c.V, c.XMLSpace = trimCellValue(val, false)
}

// getCellDate parse cell value which containing a boolean.
func (c *xlsxC) getCellBool(f *File, raw bool) (string, error) {
	if !raw {
		if c.V == "1" {
			return "TRUE", nil
		}
		if c.V == "0" {
			return "FALSE", nil
		}
	}
	return f.formattedValue(c, raw, CellTypeBool)
}

// setCellDefault prepares cell type and string type cell value by a given
// string.
func (c *xlsxC) setCellDefault(value string) {
	if ok, _, _ := isNumeric(value); !ok {
		if value != "" {
			c.setInlineStr(value)
			c.IS.T.Val = value
			return
		}
		c.T, c.V, c.IS = value, value, nil
		return
	}
	c.T, c.V = "", value
}

// getCellDate parse cell value which contains a date in the ISO 8601 format.
func (c *xlsxC) getCellDate(f *File, raw bool) (string, error) {
	if !raw {
		layout := "20060102T150405.999"
		if strings.HasSuffix(c.V, "Z") {
			layout = "20060102T150405Z"
			if strings.Contains(c.V, "-") {
				layout = "2006-01-02T15:04:05Z"
			}
		} else if strings.Contains(c.V, "-") {
			layout = "2006-01-02 15:04:05Z"
		}
		if timestamp, err := time.Parse(layout, strings.ReplaceAll(c.V, ",", ".")); err == nil {
			excelTime, _ := timeToExcelTime(timestamp, false)
			c.V = strconv.FormatFloat(excelTime, 'G', 15, 64)
		}
	}
	return f.formattedValue(c, raw, CellTypeDate)
}

// getValueFrom return a value from a column/row cell, this function is
// intended to be used with for range on rows an argument with the spreadsheet
// opened file.
func (c *xlsxC) getValueFrom(f *File, d *xlsxSST, raw bool) (string, error) {
	switch c.T {
	case "b":
		return c.getCellBool(f, raw)
	case "d":
		return c.getCellDate(f, raw)
	case "s":
		if c.V != "" {
			xlsxSI, _ := strconv.Atoi(strings.TrimSpace(c.V))
			if _, ok := f.tempFiles.Load(defaultXMLPathSharedStrings); ok {
				return f.formattedValue(&xlsxC{S: c.S, V: f.getFromStringItem(xlsxSI)}, raw, CellTypeSharedString)
			}
			d.mu.Lock()
			defer d.mu.Unlock()
			if len(d.SI) > xlsxSI {
				return f.formattedValue(&xlsxC{S: c.S, V: d.SI[xlsxSI].String()}, raw, CellTypeSharedString)
			}
		}
		return f.formattedValue(c, raw, CellTypeSharedString)
	case "str":
		return c.V, nil
	case "inlineStr":
		if c.IS != nil {
			return f.formattedValue(&xlsxC{S: c.S, V: c.IS.String()}, raw, CellTypeInlineString)
		}
		return f.formattedValue(c, raw, CellTypeInlineString)
	default:
		if isNum, precision, decimal := isNumeric(c.V); isNum && !raw {
			if precision > 15 {
				c.V = strconv.FormatFloat(decimal, 'G', 15, 64)
			} else {
				c.V = strconv.FormatFloat(decimal, 'f', -1, 64)
			}
		}
		return f.formattedValue(c, raw, CellTypeNumber)
	}
}

// SetCellDefault provides a function to set string type value of a cell as
// default format without escaping the cell.
func (f *File) SetCellDefault(sheet, cell, value string) error {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	c.setCellDefault(value)
	return f.removeFormula(c, ws, sheet)
}

// GetCellFormula provides a function to get formula from cell by given
// worksheet name and cell reference in spreadsheet.
func (f *File) GetCellFormula(sheet, cell string) (string, error) {
	return f.getCellStringFunc(sheet, cell, func(x *xlsxWorksheet, c *xlsxC) (string, bool, error) {
		if c.F == nil {
			return "", false, nil
		}
		if c.F.T == STCellFormulaTypeShared && c.F.Si != nil {
			return getSharedFormula(x, *c.F.Si, c.R), true, nil
		}
		return c.F.Content, true, nil
	})
}

// FormulaOpts can be passed to SetCellFormula to use other formula types.
type FormulaOpts struct {
	Type *string // Formula type
	Ref  *string // Shared formula ref
}

// SetCellFormula provides a function to set formula on the cell is taken
// according to the given worksheet name and cell formula settings. The result
// of the formula cell can be calculated when the worksheet is opened by the
// Office Excel application or can be using the "CalcCellValue" function also
// can get the calculated cell value. If the Excel application doesn't
// calculate the formula automatically when the workbook has been opened,
// please call "UpdateLinkedValue" after setting the cell formula functions.
//
// Example 1, set normal formula "=SUM(A1,B1)" for the cell "A3" on "Sheet1":
//
//	err := f.SetCellFormula("Sheet1", "A3", "=SUM(A1,B1)")
//
// Example 2, set one-dimensional vertical constant array (column array) formula
// "1,2,3" for the cell "A3" on "Sheet1":
//
//	err := f.SetCellFormula("Sheet1", "A3", "={1;2;3}")
//
// Example 3, set one-dimensional horizontal constant array (row array)
// formula '"a","b","c"' for the cell "A3" on "Sheet1":
//
//	err := f.SetCellFormula("Sheet1", "A3", "={\"a\",\"b\",\"c\"}")
//
// Example 4, set two-dimensional constant array formula '{1,2,"a","b"}' for
// the cell "A3" on "Sheet1":
//
//	formulaType, ref := excelize.STCellFormulaTypeArray, "A3:A3"
//	err := f.SetCellFormula("Sheet1", "A3", "={1,2;\"a\",\"b\"}",
//	    excelize.FormulaOpts{Ref: &ref, Type: &formulaType})
//
// Example 5, set range array formula "A1:A2" for the cell "A3" on "Sheet1":
//
//	formulaType, ref := excelize.STCellFormulaTypeArray, "A3:A3"
//	err := f.SetCellFormula("Sheet1", "A3", "=A1:A2",
//	       excelize.FormulaOpts{Ref: &ref, Type: &formulaType})
//
// Example 6, set shared formula "=A1+B1" for the cell "C1:C5"
// on "Sheet1", "C1" is the master cell:
//
//	formulaType, ref := excelize.STCellFormulaTypeShared, "C1:C5"
//	err := f.SetCellFormula("Sheet1", "C1", "=A1+B1",
//	    excelize.FormulaOpts{Ref: &ref, Type: &formulaType})
//
// Example 7, set table formula "=SUM(Table1[[A]:[B]])" for the cell "C2"
// on "Sheet1":
//
//	package main
//
//	import (
//	    "fmt"
//
//	    "github.com/xuri/excelize/v2"
//	)
//
//	func main() {
//	    f := excelize.NewFile()
//	    defer func() {
//	        if err := f.Close(); err != nil {
//	            fmt.Println(err)
//	        }
//	    }()
//	    for idx, row := range [][]interface{}{{"A", "B", "C"}, {1, 2}} {
//	        if err := f.SetSheetRow("Sheet1", fmt.Sprintf("A%d", idx+1), &row); err != nil {
//	            fmt.Println(err)
//	            return
//	        }
//	    }
//	    if err := f.AddTable("Sheet1", &excelize.Table{
//	        Range: "A1:C2", Name: "Table1", StyleName: "TableStyleMedium2",
//	    }); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    formulaType := excelize.STCellFormulaTypeDataTable
//	    if err := f.SetCellFormula("Sheet1", "C2", "=SUM(Table1[[A]:[B]])",
//	        excelize.FormulaOpts{Type: &formulaType}); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    if err := f.SaveAs("Book1.xlsx"); err != nil {
//	        fmt.Println(err)
//	    }
//	}
func (f *File) SetCellFormula(sheet, cell, formula string, opts ...FormulaOpts) error {
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return err
	}
	c, _, _, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	if formula == "" {
		c.F = nil
		return f.deleteCalcChain(f.getSheetID(sheet), cell)
	}

	if c.F != nil {
		c.F.Content = formula
	} else {
		c.F = &xlsxF{Content: formula}
	}

	for _, opt := range opts {
		if opt.Type != nil {
			if *opt.Type == STCellFormulaTypeDataTable {
				return err
			}
			c.F.T = *opt.Type
			if c.F.T == STCellFormulaTypeShared {
				if err = ws.setSharedFormula(*opt.Ref); err != nil {
					return err
				}
			}
		}
		if opt.Ref != nil {
			c.F.Ref = *opt.Ref
		}
	}
	c.T, c.IS = "str", nil
	return err
}

// setSharedFormula set shared formula for the cells.
func (ws *xlsxWorksheet) setSharedFormula(ref string) error {
	coordinates, err := rangeRefToCoordinates(ref)
	if err != nil {
		return err
	}
	_ = sortCoordinates(coordinates)
	cnt := ws.countSharedFormula()
	for c := coordinates[0]; c <= coordinates[2]; c++ {
		for r := coordinates[1]; r <= coordinates[3]; r++ {
			ws.prepareSheetXML(c, r)
			cell := &ws.SheetData.Row[r-1].C[c-1]
			if cell.F == nil {
				cell.F = &xlsxF{}
			}
			cell.F.T = STCellFormulaTypeShared
			cell.F.Si = &cnt
		}
	}
	return err
}

// countSharedFormula count shared formula in the given worksheet.
func (ws *xlsxWorksheet) countSharedFormula() (count int) {
	for _, row := range ws.SheetData.Row {
		for _, cell := range row.C {
			if cell.F != nil && cell.F.Si != nil && *cell.F.Si+1 > count {
				count = *cell.F.Si + 1
			}
		}
	}
	return
}

// GetCellHyperLink gets a cell hyperlink based on the given worksheet name and
// cell reference. If the cell has a hyperlink, it will return 'true' and
// the link address, otherwise it will return 'false' and an empty link
// address.
//
// For example, get a hyperlink to a 'H6' cell on a worksheet named 'Sheet1':
//
//	link, target, err := f.GetCellHyperLink("Sheet1", "H6")
func (f *File) GetCellHyperLink(sheet, cell string) (bool, string, error) {
	// Check for correct cell name
	if _, _, err := SplitCellName(cell); err != nil {
		return false, "", err
	}
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return false, "", err
	}
	if ws.Hyperlinks != nil {
		for _, link := range ws.Hyperlinks.Hyperlink {
			ok, err := f.checkCellInRangeRef(cell, link.Ref)
			if err != nil {
				return false, "", err
			}
			if link.Ref == cell || ok {
				if link.RID != "" {
					return true, f.getSheetRelationshipsTargetByID(sheet, link.RID), err
				}
				return true, link.Location, err
			}
		}
	}
	return false, "", err
}

// HyperlinkOpts can be passed to SetCellHyperlink to set optional hyperlink
// attributes (e.g. display value)
type HyperlinkOpts struct {
	Display *string
	Tooltip *string
}

// SetCellHyperLink provides a function to set cell hyperlink by given
// worksheet name and link URL address. LinkType defines two types of
// hyperlink "External" for website or "Location" for moving to one of cell in
// this workbook. Maximum limit hyperlinks in a worksheet is 65530. This
// function is only used to set the hyperlink of the cell and doesn't affect
// the value of the cell. If you need to set the value of the cell, please use
// the other functions such as `SetCellStyle` or `SetSheetRow`. The below is
// example for external link.
//
//	display, tooltip := "https://github.com/xuri/excelize", "Excelize on GitHub"
//	if err := f.SetCellHyperLink("Sheet1", "A3",
//	    display, "External", excelize.HyperlinkOpts{
//	        Display: &display,
//	        Tooltip: &tooltip,
//	    }); err != nil {
//	    fmt.Println(err)
//	}
//	// Set underline and font color style for the cell.
//	style, err := f.NewStyle(&excelize.Style{
//	    Font: &excelize.Font{Color: "1265BE", Underline: "single"},
//	})
//	if err != nil {
//	    fmt.Println(err)
//	}
//	err = f.SetCellStyle("Sheet1", "A3", "A3", style)
//
// This is another example for "Location":
//
//	err := f.SetCellHyperLink("Sheet1", "A3", "Sheet1!A40", "Location")
func (f *File) SetCellHyperLink(sheet, cell, link, linkType string, opts ...HyperlinkOpts) error {
	// Check for correct cell name
	if _, _, err := SplitCellName(cell); err != nil {
		return err
	}

	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return err
	}
	if cell, err = ws.mergeCellsParser(cell); err != nil {
		return err
	}

	var linkData xlsxHyperlink
	idx := -1
	if ws.Hyperlinks == nil {
		ws.Hyperlinks = new(xlsxHyperlinks)
	}
	for i, hyperlink := range ws.Hyperlinks.Hyperlink {
		if hyperlink.Ref == cell {
			idx = i
			linkData = hyperlink
			break
		}
	}

	if len(ws.Hyperlinks.Hyperlink) > TotalSheetHyperlinks {
		return ErrTotalSheetHyperlinks
	}

	switch linkType {
	case "External":
		sheetPath, _ := f.getSheetXMLPath(sheet)
		sheetRels := "xl/worksheets/_rels/" + strings.TrimPrefix(sheetPath, "xl/worksheets/") + ".rels"
		rID := f.setRels(linkData.RID, sheetRels, SourceRelationshipHyperLink, link, linkType)
		linkData = xlsxHyperlink{
			Ref: cell,
		}
		linkData.RID = "rId" + strconv.Itoa(rID)
		f.addSheetNameSpace(sheet, SourceRelationship)
	case "Location":
		linkData = xlsxHyperlink{
			Ref:      cell,
			Location: link,
		}
	default:
		return newInvalidLinkTypeError(linkType)
	}

	for _, o := range opts {
		if o.Display != nil {
			linkData.Display = *o.Display
		}
		if o.Tooltip != nil {
			linkData.Tooltip = *o.Tooltip
		}
	}
	if idx == -1 {
		ws.Hyperlinks.Hyperlink = append(ws.Hyperlinks.Hyperlink, linkData)
		return err
	}
	ws.Hyperlinks.Hyperlink[idx] = linkData
	return err
}

// getCellRichText returns rich text of cell by given string item.
func getCellRichText(si *xlsxSI) (runs []RichTextRun) {
	for _, v := range si.R {
		run := RichTextRun{
			Text: v.T.Val,
		}
		if v.RPr != nil {
			run.Font = newFont(v.RPr)
		}
		runs = append(runs, run)
	}
	return
}

// GetCellRichText provides a function to get rich text of cell by given
// worksheet.
func (f *File) GetCellRichText(sheet, cell string) (runs []RichTextRun, err error) {
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return
	}
	c, _, _, err := ws.prepareCell(cell)
	if err != nil {
		return
	}
	siIdx, err := strconv.Atoi(c.V)
	if err != nil || c.T != "s" {
		return
	}
	sst, err := f.sharedStringsReader()
	if err != nil {
		return
	}
	if len(sst.SI) <= siIdx || siIdx < 0 {
		return
	}
	runs = getCellRichText(&sst.SI[siIdx])
	return
}

// newRpr create run properties for the rich text by given font format.
func newRpr(fnt *Font) *xlsxRPr {
	rpr := xlsxRPr{}
	trueVal := ""
	if fnt.Bold {
		rpr.B = &trueVal
	}
	if fnt.Italic {
		rpr.I = &trueVal
	}
	if fnt.Strike {
		rpr.Strike = &trueVal
	}
	if fnt.Underline != "" {
		rpr.U = &attrValString{Val: &fnt.Underline}
	}
	if fnt.Family != "" {
		rpr.RFont = &attrValString{Val: &fnt.Family}
	}
	if inStrSlice([]string{"baseline", "superscript", "subscript"}, fnt.VertAlign, true) != -1 {
		rpr.VertAlign = &attrValString{Val: &fnt.VertAlign}
	}
	if fnt.Size > 0 {
		rpr.Sz = &attrValFloat{Val: &fnt.Size}
	}
	rpr.Color = newFontColor(fnt)
	return &rpr
}

// newFont create font format by given run properties for the rich text.
func newFont(rPr *xlsxRPr) *Font {
	font := Font{Underline: "none"}
	font.Bold = rPr.B != nil
	font.Italic = rPr.I != nil
	if rPr.U != nil {
		font.Underline = "single"
		if rPr.U.Val != nil {
			font.Underline = *rPr.U.Val
		}
	}
	if rPr.RFont != nil && rPr.RFont.Val != nil {
		font.Family = *rPr.RFont.Val
	}
	if rPr.Sz != nil && rPr.Sz.Val != nil {
		font.Size = *rPr.Sz.Val
	}
	font.Strike = rPr.Strike != nil
	if rPr.Color != nil {
		font.Color = strings.TrimPrefix(rPr.Color.RGB, "FF")
		if rPr.Color.Theme != nil {
			font.ColorTheme = rPr.Color.Theme
		}
		font.ColorIndexed = rPr.Color.Indexed
		font.ColorTint = rPr.Color.Tint
	}
	return &font
}

// setRichText provides a function to set rich text of a cell.
func setRichText(runs []RichTextRun) ([]xlsxR, error) {
	var (
		textRuns       []xlsxR
		totalCellChars int
	)
	for _, textRun := range runs {
		totalCellChars += len(textRun.Text)
		if totalCellChars > TotalCellChars {
			return textRuns, ErrCellCharsLength
		}
		run := xlsxR{T: &xlsxT{}}
		run.T.Val, run.T.Space = trimCellValue(textRun.Text, false)
		fnt := textRun.Font
		if fnt != nil {
			run.RPr = newRpr(fnt)
		}
		textRuns = append(textRuns, run)
	}
	return textRuns, nil
}

// SetCellRichText provides a function to set cell with rich text by given
// worksheet. For example, set rich text on the A1 cell of the worksheet named
// Sheet1:
//
//	package main
//
//	import (
//	    "fmt"
//
//	    "github.com/xuri/excelize/v2"
//	)
//
//	func main() {
//	    f := excelize.NewFile()
//	    defer func() {
//	        if err := f.Close(); err != nil {
//	            fmt.Println(err)
//	        }
//	    }()
//	    if err := f.SetRowHeight("Sheet1", 1, 35); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    if err := f.SetColWidth("Sheet1", "A", "A", 44); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    if err := f.SetCellRichText("Sheet1", "A1", []excelize.RichTextRun{
//	        {
//	            Text: "bold",
//	            Font: &excelize.Font{
//	                Bold:   true,
//	                Color:  "2354e8",
//	                Family: "Times New Roman",
//	            },
//	        },
//	        {
//	            Text: " and ",
//	            Font: &excelize.Font{
//	                Family: "Times New Roman",
//	            },
//	        },
//	        {
//	            Text: "italic ",
//	            Font: &excelize.Font{
//	                Bold:   true,
//	                Color:  "e83723",
//	                Italic: true,
//	                Family: "Times New Roman",
//	            },
//	        },
//	        {
//	            Text: "text with color and font-family,",
//	            Font: &excelize.Font{
//	                Bold:   true,
//	                Color:  "2354e8",
//	                Family: "Times New Roman",
//	            },
//	        },
//	        {
//	            Text: "\r\nlarge text with ",
//	            Font: &excelize.Font{
//	                Size:  14,
//	                Color: "ad23e8",
//	            },
//	        },
//	        {
//	            Text: "strike",
//	            Font: &excelize.Font{
//	                Color:  "e89923",
//	                Strike: true,
//	            },
//	        },
//	        {
//	            Text: " superscript",
//	            Font: &excelize.Font{
//	                Color:     "dbc21f",
//	                VertAlign: "superscript",
//	            },
//	        },
//	        {
//	            Text: " and ",
//	            Font: &excelize.Font{
//	                Size:      14,
//	                Color:     "ad23e8",
//	                VertAlign: "baseline",
//	            },
//	        },
//	        {
//	            Text: "underline",
//	            Font: &excelize.Font{
//	                Color:     "23e833",
//	                Underline: "single",
//	            },
//	        },
//	        {
//	            Text: " subscript.",
//	            Font: &excelize.Font{
//	                Color:     "017505",
//	                VertAlign: "subscript",
//	            },
//	        },
//	    }); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    style, err := f.NewStyle(&excelize.Style{
//	        Alignment: &excelize.Alignment{
//	            WrapText: true,
//	        },
//	    })
//	    if err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    if err := f.SetCellStyle("Sheet1", "A1", "A1", style); err != nil {
//	        fmt.Println(err)
//	        return
//	    }
//	    if err := f.SaveAs("Book1.xlsx"); err != nil {
//	        fmt.Println(err)
//	    }
//	}
func (f *File) SetCellRichText(sheet, cell string, runs []RichTextRun) error {
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		return err
	}
	c, col, row, err := ws.prepareCell(cell)
	if err != nil {
		return err
	}
	if err := f.sharedStringsLoader(); err != nil {
		return err
	}
	c.S = ws.prepareCellStyle(col, row, c.S)
	si := xlsxSI{}
	sst, err := f.sharedStringsReader()
	if err != nil {
		return err
	}
	if si.R, err = setRichText(runs); err != nil {
		return err
	}
	for idx, strItem := range sst.SI {
		if reflect.DeepEqual(strItem, si) {
			c.T, c.V = "s", strconv.Itoa(idx)
			return err
		}
	}
	sst.SI = append(sst.SI, si)
	sst.Count++
	sst.UniqueCount++
	c.T, c.V = "s", strconv.Itoa(len(sst.SI)-1)
	return err
}

// SetSheetRow writes an array to row by given worksheet name, starting
// cell reference and a pointer to array type 'slice'. This function is
// concurrency safe. For example, writes an array to row 6 start with the cell
// B6 on Sheet1:
//
//	err := f.SetSheetRow("Sheet1", "B6", &[]interface{}{"1", nil, 2})
func (f *File) SetSheetRow(sheet, cell string, slice interface{}) error {
	return f.setSheetCells(sheet, cell, slice, rows)
}

// SetSheetCol writes an array to column by given worksheet name, starting
// cell reference and a pointer to array type 'slice'. For example, writes an
// array to column B start with the cell B6 on Sheet1:
//
//	err := f.SetSheetCol("Sheet1", "B6", &[]interface{}{"1", nil, 2})
func (f *File) SetSheetCol(sheet, cell string, slice interface{}) error {
	return f.setSheetCells(sheet, cell, slice, columns)
}

// setSheetCells provides a function to set worksheet cells value.
func (f *File) setSheetCells(sheet, cell string, slice interface{}, dir adjustDirection) error {
	col, row, err := CellNameToCoordinates(cell)
	if err != nil {
		return err
	}
	// Make sure 'slice' is a Ptr to Slice
	v := reflect.ValueOf(slice)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return ErrParameterInvalid
	}
	v = v.Elem()
	for i := 0; i < v.Len(); i++ {
		var cell string
		var err error
		if dir == rows {
			cell, err = CoordinatesToCellName(col+i, row)
		} else {
			cell, err = CoordinatesToCellName(col, row+i)
		}
		// Error should never happen here. But keep checking to early detect regressions
		// if it will be introduced in the future.
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, v.Index(i).Interface()); err != nil {
			return err
		}
	}
	return err
}

// getCellInfo does common preparation for all set cell value functions.
func (ws *xlsxWorksheet) prepareCell(cell string) (*xlsxC, int, int, error) {
	var err error
	cell, err = ws.mergeCellsParser(cell)
	if err != nil {
		return nil, 0, 0, err
	}
	col, row, err := CellNameToCoordinates(cell)
	if err != nil {
		return nil, 0, 0, err
	}

	ws.prepareSheetXML(col, row)
	return &ws.SheetData.Row[row-1].C[col-1], col, row, err
}

// getCellStringFunc does common value extraction workflow for all get cell
// value function. Passed function implements specific part of required
// logic.
func (f *File) getCellStringFunc(sheet, cell string, fn func(x *xlsxWorksheet, c *xlsxC) (string, bool, error)) (string, error) {
	f.mu.Lock()
	ws, err := f.workSheetReader(sheet)
	if err != nil {
		f.mu.Unlock()
		return "", err
	}
	f.mu.Unlock()
	ws.mu.Lock()
	defer ws.mu.Unlock()
	cell, err = ws.mergeCellsParser(cell)
	if err != nil {
		return "", err
	}
	_, row, err := CellNameToCoordinates(cell)
	if err != nil {
		return "", err
	}
	lastRowNum := 0
	if l := len(ws.SheetData.Row); l > 0 {
		lastRowNum = ws.SheetData.Row[l-1].R
	}

	// keep in mind: row starts from 1
	if row > lastRowNum {
		return "", nil
	}

	for rowIdx := range ws.SheetData.Row {
		rowData := &ws.SheetData.Row[rowIdx]
		if rowData.R != row {
			continue
		}
		for colIdx := range rowData.C {
			colData := &rowData.C[colIdx]
			if cell != colData.R {
				continue
			}
			val, ok, err := fn(ws, colData)
			if err != nil {
				return "", err
			}
			if ok {
				return val, nil
			}
		}
	}
	return "", nil
}

// formattedValue provides a function to returns a value after formatted. If
// it is possible to apply a format to the cell value, it will do so, if not
// then an error will be returned, along with the raw value of the cell.
func (f *File) formattedValue(c *xlsxC, raw bool, cellType CellType) (string, error) {
	if raw || c.S == 0 {
		return c.V, nil
	}
	styleSheet, err := f.stylesReader()
	if err != nil {
		return c.V, err
	}
	if styleSheet.CellXfs == nil {
		return c.V, err
	}
	if c.S >= len(styleSheet.CellXfs.Xf) || c.S < 0 {
		return c.V, err
	}
	var numFmtID int
	if styleSheet.CellXfs.Xf[c.S].NumFmtID != nil {
		numFmtID = *styleSheet.CellXfs.Xf[c.S].NumFmtID
	}
	date1904 := false
	wb, err := f.workbookReader()
	if err != nil {
		return c.V, err
	}
	if wb != nil && wb.WorkbookPr != nil {
		date1904 = wb.WorkbookPr.Date1904
	}
	if fmtCode, ok := styleSheet.getCustomNumFmtCode(numFmtID); ok {
		return format(c.V, fmtCode, date1904, cellType, f.options), err
	}
	if fmtCode, ok := f.getBuiltInNumFmtCode(numFmtID); ok {
		return f.applyBuiltInNumFmt(c, fmtCode, numFmtID, date1904, cellType), err
	}
	return c.V, err
}

// getCustomNumFmtCode provides a function to returns custom number format code.
func (ss *xlsxStyleSheet) getCustomNumFmtCode(numFmtID int) (string, bool) {
	if ss.NumFmts == nil {
		return "", false
	}
	for _, xlsxFmt := range ss.NumFmts.NumFmt {
		if xlsxFmt.NumFmtID == numFmtID {
			if xlsxFmt.FormatCode16 != "" {
				return xlsxFmt.FormatCode16, true
			}
			return xlsxFmt.FormatCode, true
		}
	}
	return "", false
}

// prepareCellStyle provides a function to prepare style index of cell in
// worksheet by given column index and style index.
func (ws *xlsxWorksheet) prepareCellStyle(col, row, style int) int {
	if style != 0 {
		return style
	}
	if row <= len(ws.SheetData.Row) {
		if styleID := ws.SheetData.Row[row-1].S; styleID != 0 {
			return styleID
		}
	}
	if ws.Cols != nil {
		for _, c := range ws.Cols.Col {
			if c.Min <= col && col <= c.Max && c.Style != 0 {
				return c.Style
			}
		}
	}
	return style
}

// mergeCellsParser provides a function to check merged cells in worksheet by
// given cell reference.
func (ws *xlsxWorksheet) mergeCellsParser(cell string) (string, error) {
	cell = strings.ToUpper(cell)
	col, row, err := CellNameToCoordinates(cell)
	if err != nil {
		return cell, err
	}
	if ws.MergeCells != nil {
		for i := 0; i < len(ws.MergeCells.Cells); i++ {
			if ws.MergeCells.Cells[i] == nil {
				ws.MergeCells.Cells = append(ws.MergeCells.Cells[:i], ws.MergeCells.Cells[i+1:]...)
				i--
				continue
			}
			if ref := ws.MergeCells.Cells[i].Ref; len(ws.MergeCells.Cells[i].rect) == 0 && ref != "" {
				if strings.Count(ref, ":") != 1 {
					ref += ":" + ref
				}
				rect, err := rangeRefToCoordinates(ref)
				if err != nil {
					return cell, err
				}
				_ = sortCoordinates(rect)
				ws.MergeCells.Cells[i].rect = rect
			}
			if cellInRange([]int{col, row}, ws.MergeCells.Cells[i].rect) {
				cell = strings.Split(ws.MergeCells.Cells[i].Ref, ":")[0]
				break
			}
		}
	}
	return cell, nil
}

// checkCellInRangeRef provides a function to determine if a given cell reference
// in a range.
func (f *File) checkCellInRangeRef(cell, rangeRef string) (bool, error) {
	col, row, err := CellNameToCoordinates(cell)
	if err != nil {
		return false, err
	}

	if rng := strings.Split(rangeRef, ":"); len(rng) != 2 {
		return false, err
	}
	coordinates, err := rangeRefToCoordinates(rangeRef)
	if err != nil {
		return false, err
	}

	return cellInRange([]int{col, row}, coordinates), err
}

// cellInRange provides a function to determine if a given range is within a
// range.
func cellInRange(cell, ref []int) bool {
	return cell[0] >= ref[0] && cell[0] <= ref[2] && cell[1] >= ref[1] && cell[1] <= ref[3]
}

// isOverlap find if the given two rectangles overlap or not.
func isOverlap(rect1, rect2 []int) bool {
	return cellInRange([]int{rect1[0], rect1[1]}, rect2) ||
		cellInRange([]int{rect1[2], rect1[1]}, rect2) ||
		cellInRange([]int{rect1[0], rect1[3]}, rect2) ||
		cellInRange([]int{rect1[2], rect1[3]}, rect2) ||
		cellInRange([]int{rect2[0], rect2[1]}, rect1) ||
		cellInRange([]int{rect2[2], rect2[1]}, rect1) ||
		cellInRange([]int{rect2[0], rect2[3]}, rect1) ||
		cellInRange([]int{rect2[2], rect2[3]}, rect1)
}

// parseSharedFormula generate dynamic part of shared formula for target cell
// by given column and rows distance and origin shared formula.
func parseSharedFormula(dCol, dRow int, orig []byte) (res string, start int) {
	var (
		end           int
		stringLiteral bool
	)
	for end = 0; end < len(orig); end++ {
		c := orig[end]
		if c == '"' {
			stringLiteral = !stringLiteral
		}
		if stringLiteral {
			continue // Skip characters in quotes
		}
		if c >= 'A' && c <= 'Z' || c == '$' {
			res += string(orig[start:end])
			start = end
			end++
			foundNum := false
			for ; end < len(orig); end++ {
				idc := orig[end]
				if idc >= '0' && idc <= '9' || idc == '$' {
					foundNum = true
				} else if idc >= 'A' && idc <= 'Z' {
					if foundNum {
						break
					}
				} else {
					break
				}
			}
			if foundNum {
				cellID := string(orig[start:end])
				res += shiftCell(cellID, dCol, dRow)
				start = end
			}
		}
	}
	return
}

// getSharedFormula find a cell contains the same formula as another cell,
// the "shared" value can be used for the t attribute and the si attribute can
// be used to refer to the cell containing the formula. Two formulas are
// considered to be the same when their respective representations in
// R1C1-reference notation, are the same.
//
// Note that this function not validate ref tag to check the cell whether in
// allow range reference, and always return origin shared formula.
func getSharedFormula(ws *xlsxWorksheet, si int, cell string) string {
	for _, r := range ws.SheetData.Row {
		for _, c := range r.C {
			if c.F != nil && c.F.Ref != "" && c.F.T == STCellFormulaTypeShared && c.F.Si != nil && *c.F.Si == si {
				col, row, _ := CellNameToCoordinates(cell)
				sharedCol, sharedRow, _ := CellNameToCoordinates(c.R)
				dCol := col - sharedCol
				dRow := row - sharedRow
				orig := []byte(c.F.Content)
				res, start := parseSharedFormula(dCol, dRow, orig)
				if start < len(orig) {
					res += string(orig[start:])
				}
				return res
			}
		}
	}
	return ""
}

// shiftCell returns the cell shifted according to dCol and dRow taking into
// consideration absolute references with dollar sign ($)
func shiftCell(cellID string, dCol, dRow int) string {
	fCol, fRow, _ := CellNameToCoordinates(cellID)
	signCol, signRow := "", ""
	if strings.Index(cellID, "$") == 0 {
		signCol = "$"
	} else {
		// Shift column
		fCol += dCol
	}
	if strings.LastIndex(cellID, "$") > 0 {
		signRow = "$"
	} else {
		// Shift row
		fRow += dRow
	}
	colName, _ := ColumnNumberToName(fCol)
	return signCol + colName + signRow + strconv.Itoa(fRow)
}