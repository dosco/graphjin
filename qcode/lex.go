package qcode

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pos represents a byte position in the original input text from which
// this template was parsed.
type Pos int

func (p Pos) Position() Pos {
	return p
}

// item represents a token or text string returned from the scanner.
type item struct {
	typ  itemType // The type of this item.
	pos  Pos      // The starting position, in bytes, of this item in the input string.
	val  string   // The value of this item.
	line int      // The line number at the start of this item.
}

func (i *item) String() string {
	var v string

	switch i.typ {
	case itemEOF:
		v = "EOF"
	case itemError:
		v = "error"
	case itemName:
		v = "name"
	case itemQuery:
		v = "query"
	case itemMutation:
		v = "mutation"
	case itemSub:
		v = "subscription"
	case itemPunctuator:
		v = "punctuator"
	case itemDirective:
		v = "directive"
	case itemVariable:
		v = "variable"
	case itemIntVal:
		v = "int"
	case itemFloatVal:
		v = "float"
	case itemStringVal:
		v = "string"
	}
	return fmt.Sprintf("%s %q", v, i.val)
}

// itemType identifies the type of lex items.
type itemType int

const (
	itemError itemType = iota // error occurred; value is text of error
	itemEOF
	itemName
	itemQuery
	itemMutation
	itemSub
	itemPunctuator
	itemArgsOpen
	itemArgsClose
	itemListOpen
	itemListClose
	itemObjOpen
	itemObjClose
	itemColon
	itemEquals
	itemDirective
	itemVariable
	itemSpread
	itemIntVal
	itemFloatVal
	itemStringVal
	itemBoolVal
)

// !$():=@[]{|}
var punctuators = map[rune]itemType{
	'{': itemObjOpen,
	'}': itemObjClose,
	'[': itemListOpen,
	']': itemListClose,
	'(': itemArgsOpen,
	')': itemArgsClose,
	':': itemColon,
	'=': itemEquals,
}

const eof = -1

// stateFn represents the state of the scanner as a function that returns the next state.
type stateFn func(*lexer) stateFn

// lexer holds the state of the scanner.
type lexer struct {
	name   string // the name of the input; used only for error reports
	input  string // the string being scanned
	pos    Pos    // current position in the input
	start  Pos    // start position of this item
	width  Pos    // width of last rune read from input
	items  []item // array of scanned items
	itemsA [100]item
	line   int // 1+number of newlines seen
}

var zeroLex = lexer{}

func (l *lexer) Reset() {
	*l = zeroLex
}

// next returns the next rune in the input.
func (l *lexer) next() rune {
	if int(l.pos) >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRuneInString(l.input[l.pos:])
	l.width = Pos(w)
	l.pos += l.width
	if r == '\n' {
		l.line++
	}
	return r
}

// peek returns but does not consume the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// backup steps back one rune. Can only be called once per call of next.
func (l *lexer) backup() {
	l.pos -= l.width
	// Correct newline count.
	if l.width == 1 && l.input[l.pos] == '\n' {
		l.line--
	}
}

func (l *lexer) current() string {
	return l.input[l.start:l.pos]
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.items = append(l.items, item{t, l.start, l.input[l.start:l.pos], l.line})
	// Some items contain text internally. If so, count their newlines.
	switch t {
	case itemName:
		l.line += strings.Count(l.input[l.start:l.pos], "\n")
	}
	l.start = l.pos
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.line += strings.Count(l.input[l.start:l.pos], "\n")
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid string) bool {
	if strings.ContainsRune(valid, l.next()) {
		return true
	}
	l.backup()
	return false
}

// acceptAlphaNum consumes a run of runes while they are alpha nums
func (l *lexer) acceptAlphaNum() bool {
	n := 0
	for r := l.next(); isAlphaNumeric(r); r = l.next() {
		n++
	}
	l.backup()
	return (n != 0)
}

// acceptComment consumes a run of runes while till the end of line
func (l *lexer) acceptComment() {
	n := 0
	for r := l.next(); !isEndOfLine(r); r = l.next() {
		n++
	}
}

// acceptRun consumes a run of runes from the valid set.
func (l *lexer) acceptRun(valid string) {
	for strings.ContainsRune(valid, l.next()) {
	}
	l.backup()
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating l.nextItem.
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.items = append(l.items, item{itemError, l.start,
		fmt.Sprintf(format, args...), l.line})
	return nil
}

// lex creates a new scanner for the input string.
func lex(l *lexer, input string) error {
	if len(input) == 0 {
		return errors.New("empty query")
	}
	l.input = input
	l.line = 1
	l.items = l.itemsA[:0]
	l.run()

	if last := l.items[len(l.items)-1]; last.typ == itemError {
		return fmt.Errorf(last.val)
	}
	return nil
}

// run runs the state machine for the lexer.
func (l *lexer) run() {
	for state := lexRoot; state != nil; {
		state = state(l)
	}
}

// lexInsideAction scans the elements inside action delimiters.
func lexRoot(l *lexer) stateFn {
	r := l.next()

	switch {
	case r == eof:
		l.emit(itemEOF)
		return nil
	case isEndOfLine(r):
		l.ignore()
	case isSpace(r):
		l.ignore()
	case r == '#':
		l.ignore()
		l.acceptComment()
		l.ignore()
	case r == '@':
		l.ignore()
		if l.acceptAlphaNum() {
			l.emit(itemDirective)
		}
	case r == '$':
		l.ignore()
		if l.acceptAlphaNum() {
			l.emit(itemVariable)
		}
	case strings.ContainsRune("!():=[]{|}", r):
		if item, ok := punctuators[r]; ok {
			l.emit(item)
		} else {
			l.emit(itemPunctuator)
		}
	case r == '"' || r == '\'':
		l.backup()
		return lexString
	case r == '.':
		if len(l.input) >= 3 {
			if strings.HasSuffix(l.input[:l.pos], "...") {
				l.emit(itemSpread)
				return lexRoot
			}
		}
		fallthrough // '.' can start a number.
	case r == '+' || r == '-' || ('0' <= r && r <= '9'):
		l.backup()
		return lexNumber
	case isAlphaNumeric(r):
		l.backup()
		return lexName
	default:
		return l.errorf("unrecognized character in action: %#U", r)
	}
	return lexRoot
}

// lexName scans a name.
func lexName(l *lexer) stateFn {
	for {
		r := l.next()
		if r == eof {
			l.emit(itemEOF)
			return nil
		}
		if !isAlphaNumeric(r) {
			l.backup()
			v := l.current()

			if len(v) == 0 {
				switch {
				case strings.EqualFold(v, "query"):
					l.emit(itemQuery)
					break
				case strings.EqualFold(v, "mutation"):
					l.emit(itemMutation)
					break
				case strings.EqualFold(v, "subscription"):
					l.emit(itemSub)
					break
				}
			}

			switch {
			case strings.EqualFold(v, "true"):
				l.emit(itemBoolVal)
			case strings.EqualFold(v, "false"):
				l.emit(itemBoolVal)
			default:
				l.emit(itemName)
			}
			break
		}
	}
	return lexRoot
}

// lexString scans a string.
func lexString(l *lexer) stateFn {
	if l.accept("\"'") {
		l.ignore()

		for {
			r := l.next()
			if r == eof {
				l.emit(itemEOF)
				return nil
			}
			if r == '\'' || r == '"' {
				l.backup()
				l.emit(itemStringVal)
				if l.accept("\"'") {
					l.ignore()
				}
				break
			}
		}
	}
	return lexRoot
}

// lexNumber scans a number: decimal, octal, hex, float, or imaginary. This
// isn't a perfect number scanner - for instance it accepts "." and "0x0.2"
// and "089" - but when it's wrong the input is invalid and the parser (via
// strconv) will notice.
func lexNumber(l *lexer) stateFn {
	var it itemType
	// Optional leading sign.
	l.accept("+-")

	// Is it integer
	digits := "0123456789"
	if l.accept(digits) {
		l.acceptRun(digits)
		it = itemIntVal
	}

	// Is it float
	if l.peek() == '.' {
		if l.accept(".") {
			if l.accept(digits) {
				l.acceptRun(digits)
				it = itemFloatVal
			}
		} else {
			l.backup()
		}
	}

	// Next thing mustn't be alphanumeric.
	if isAlphaNumeric(l.peek()) {
		l.next()
		return l.errorf("bad number syntax: %q", l.input[l.start:l.pos])
	}

	if it != 0 {
		l.emit(it)
	}

	return lexRoot
}

// isSpace reports whether r is a space character.
func isSpace(r rune) bool {
	return r == ',' || r == ' ' || r == '\t'
}

// isEndOfLine reports whether r is an end-of-line character.
func isEndOfLine(r rune) bool {
	return r == '\r' || r == '\n' || r == eof
}

// isAlphaNumeric reports whether r is an alphabetic, digit, or underscore.
func isAlphaNumeric(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

/*

Copyright (c) 2009 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

    * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
    * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
    * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/
