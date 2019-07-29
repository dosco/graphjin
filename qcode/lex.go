package qcode

import (
	"bytes"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

var (
	queryToken        = []byte("query")
	mutationToken     = []byte("mutation")
	subscriptionToken = []byte("subscription")
	trueToken         = []byte("true")
	falseToken        = []byte("true")
	quotesToken       = []byte(`'"`)
	signsToken        = []byte(`+-`)
	punctuatorToken   = []byte(`!():=[]{|}`)
	spreadToken       = []byte(`...`)
	digitToken        = []byte(`0123456789`)
	dotToken          = []byte(`.`)
)

// Pos represents a byte position in the original input text from which
// this template was parsed.
type Pos int

// item represents a token or text string returned from the scanner.
type item struct {
	typ  itemType // The type of this item.
	pos  Pos      // The starting position, in bytes, of this item in the input string.
	end  Pos      // The ending position, in bytes, of this item in the input string.
	line uint16   // The line number at the start of this item.
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
	input  []byte // the string being scanned
	pos    Pos    // current position in the input
	start  Pos    // start position of this item
	width  Pos    // width of last rune read from input
	items  []item // array of scanned items
	itemsA [50]item
	line   uint16 // 1+number of newlines seen
	err    error
}

var zeroLex = lexer{}

func (l *lexer) Reset() {
	*l = zeroLex
}

// next returns the next byte in the input.
func (l *lexer) next() rune {
	if int(l.pos) >= len(l.input) {
		l.width = 0
		return eof
	}
	r, w := utf8.DecodeRune(l.input[l.pos:])
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

func (l *lexer) current() (Pos, Pos) {
	return l.start, l.pos
}

// emit passes an item back to the client.
func (l *lexer) emit(t itemType) {
	l.items = append(l.items, item{t, l.start, l.pos, l.line})
	// Some items contain text internally. If so, count their newlines.
	switch t {
	case itemName:
		for i := l.start; i < l.pos; i++ {
			if l.input[i] == '\n' {
				l.line++
			}
		}
	}
	l.start = l.pos
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	for i := l.start; i < l.pos; i++ {
		if l.input[i] == '\n' {
			l.line++
		}
	}
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid []byte) bool {
	if bytes.ContainsRune(valid, l.next()) {
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
func (l *lexer) acceptRun(valid []byte) {
	for bytes.ContainsRune(valid, l.next()) {
	}
	l.backup()
}

// errorf returns an error token and terminates the scan by passing
// back a nil pointer that will be the next state, terminating l.nextItem.
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.err = fmt.Errorf(format, args...)
	l.items = append(l.items, item{itemError, l.start, l.pos, l.line})
	return nil
}

// lex creates a new scanner for the input string.
func lex(l *lexer, input []byte) error {
	if len(input) == 0 {
		return errors.New("empty query")
	}

	l.input = input
	l.items = l.itemsA[:0]
	l.line = 1

	l.run()

	if last := l.items[len(l.items)-1]; last.typ == itemError {
		return l.err
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
			s, e := l.current()
			lowercase(l.input, s, e)
			l.emit(itemVariable)
		}
	case contains(l.input, l.start, l.pos, punctuatorToken):
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
			if equals(l.input, 0, 3, spreadToken) {
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
			s, e := l.current()

			lowercase(l.input, s, e)

			switch {
			case equals(l.input, s, e, queryToken):
				l.emit(itemQuery)
			case equals(l.input, s, e, mutationToken):
				l.emit(itemMutation)
			case equals(l.input, s, e, subscriptionToken):
				l.emit(itemSub)
			case equals(l.input, s, e, trueToken):
				l.emit(itemBoolVal)
			case equals(l.input, s, e, falseToken):
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
	if l.accept([]byte(quotesToken)) {
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
				if l.accept(quotesToken) {
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
	l.accept(signsToken)

	// Is it integer
	if l.accept(digitToken) {
		l.acceptRun(digitToken)
		it = itemIntVal
	}

	// Is it float
	if l.peek() == '.' {
		if l.accept(dotToken) {
			if l.accept(digitToken) {
				l.acceptRun(digitToken)
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

func equals(b []byte, s Pos, e Pos, val []byte) bool {
	n := 0
	for i := s; i < e; i++ {
		if n >= len(val) {
			return true
		}
		switch {
		case b[i] >= 'A' && b[i] <= 'Z' && ('a'+(b[i]-'A')) != val[n]:
			return false
		case b[i] != val[n]:
			return false
		}
		n++
	}
	return true
}

func contains(b []byte, s Pos, e Pos, val []byte) bool {
	for i := s; i < e; i++ {
		for n := 0; n < len(val); n++ {
			if b[i] == val[n] {
				return true
			}
		}
	}
	return false
}

func lowercase(b []byte, s Pos, e Pos) {
	for i := s; i < e; i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] = ('a' + (b[i] - 'A'))
		}
	}
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
	return fmt.Sprintf("%s", v)
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
