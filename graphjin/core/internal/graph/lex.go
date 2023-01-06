package graph

import (
	"bytes"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
)

var (
	typeToken         = []byte("type")
	queryToken        = []byte("query")
	mutationToken     = []byte("mutation")
	fragmentToken     = []byte("fragment")
	subscriptionToken = []byte("subscription")
	onToken           = []byte("on")
	trueToken         = []byte("true")
	falseToken        = []byte("false")
	quotesToken       = []byte(`'"`)
	signsToken        = []byte(`+-`)
	spreadToken       = []byte(`...`)
	digitToken        = []byte(`0123456789`)
	dotToken          = []byte(`.`)

	punctuatorToken = `!():=[]{|}`
)

// Pos represents a byte position in the original input text from which
// this template was parsed.
type Pos int

// item represents a token or text string returned from the scanner.
type item struct {
	_type MType  // The type of this item.
	pos   Pos    // The starting position, in bytes, of this item in the input string.
	val   []byte // The value of this item.
	line  int16  // The line number at the start of this item.
}

// MType identifies the type of lex items.
type MType int8

const (
	itemError      MType = iota // error
	itemEOF                     // end of file
	itemName                    // label
	itemOn                      // "on"
	itemPunctuator              // punctuation !()[]{}:=
	itemArgsOpen                // (
	itemArgsClose               // )
	itemListOpen                // [
	itemListClose               // ]
	itemObjOpen                 // {
	itemObjClose                // }
	itemColon                   // :
	itemEquals                  // =
	itemRequired                // !
	itemDirective               // @(directive)
	itemVariable                // $variable
	itemSpread                  // ...
	itemNumberVal               // number
	itemStringVal               // string
	itemBoolVal                 // boolean
)

// !()[]{}:=
var punctuators = map[rune]MType{
	'!': itemRequired,
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
	line   int16 // 1+number of newlines seen
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
	if l.pos != 0 {
		l.pos -= l.width
		// Correct newline count.
		if l.width == 1 && l.input[l.pos] == '\n' {
			l.line--
		}
	}
}

func (l *lexer) current() []byte {
	return l.input[l.start:l.pos]
}

// emit passes an item back to the client.
func (l *lexer) emit(t MType) {
	l.items = append(l.items, item{t, l.start, l.current(), l.line})
	// Some items contain text internally. If so, count their newlines.
	if t == itemStringVal {
		for i := l.start; i < l.pos; i++ {
			if l.input[i] == '\n' {
				l.line++
			}
		}
	}
	l.start = l.pos
}

func (l *lexer) emitL(t MType) {
	lowercase(l.current())
	l.emit(t)
}

// ignore skips over the pending input before this point.
func (l *lexer) ignore() {
	l.start = l.pos
}

// accept consumes the next rune if it's from the valid set.
func (l *lexer) accept(valid []byte) (rune, bool) {
	r := l.next()
	if r != eof && bytes.ContainsRune(valid, r) {
		return r, true
	}
	l.backup()
	return r, false
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
	l.items = append(l.items, item{itemError, l.start, l.input[l.start:l.pos], l.line})
	return nil
}

// lex creates a new scanner for the input string.
func lex(input []byte) (lexer, error) {
	var l lexer

	if len(input) == 0 {
		return l, errors.New("empty query")
	}

	l.input = input
	l.items = l.itemsA[:0]
	l.line = 1

	l.run()

	if last := l.items[len(l.items)-1]; last._type == itemError {
		return l, l.err
	}
	return l, nil
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
		l.emit(itemDirective)
	case r == '!':
		l.ignore()
		l.emit(itemRequired)
	case r == '$':
		l.ignore()
		if l.acceptAlphaNum() {
			// lowercase(l.current())
			l.emit(itemVariable)
		}
	case contains(l.current(), punctuatorToken):
		if item, ok := punctuators[r]; ok {
			l.emit(item)
		}
	case r == '"' || r == '\'':
		l.backup()
		return lexString
	case r == '.':
		l.accept(dotToken)
		if equals(l.current(), spreadToken) {
			l.emit(itemSpread)
			return lexRoot
		}
		//fallthrough // '.' can start a number.
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
			val := l.current()

			switch {
			case equals(val, onToken):
				l.emitL(itemOn)
			case equals(val, trueToken):
				l.emitL(itemBoolVal)
			case equals(val, falseToken):
				l.emitL(itemBoolVal)
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
	if sr, ok := l.accept([]byte(quotesToken)); ok {
		l.ignore()

		var escaped bool
		for {
			r := l.next()
			if r == eof {
				l.emit(itemEOF)
				return nil
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if !escaped && r == sr {
				l.backup()
				l.emit(itemStringVal)
				if _, ok := l.accept(quotesToken); ok {
					l.ignore()
				}
				break
			}
			if escaped {
				escaped = false
			}
		}
	}
	return lexRoot
}

// lexNumber scans a number: decimal and float. This isn't a perfect number scanner
// for instance it accepts "." and "0x0.2" and "089" - but when it's wrong the input
// is invalid and the parser (via strconv) should notice.
func lexNumber(l *lexer) stateFn {
	if !l.scanNumber() {
		return l.errorf("bad number syntax: %q", l.input[l.start:l.pos])
	}
	l.emit(itemNumberVal)
	return lexRoot
}

func (l *lexer) scanNumber() bool {
	// Optional leading sign.
	l.accept(signsToken)
	l.acceptRun(digitToken)
	if _, ok := l.accept(dotToken); ok {
		l.acceptRun(digitToken)
	}
	// Is it imaginary?
	l.accept([]byte("i"))
	// Next thing mustn't be alphanumeric.
	if isAlphaNumeric(l.peek()) {
		l.next()
		return false
	}
	return true
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

func equals(b, val []byte) bool {
	return bytes.EqualFold(b, val)
}

func contains(b []byte, chars string) bool {
	return bytes.ContainsAny(b, chars)
}

func lowercase(b []byte) {
	for i := 0; i < len(b); i++ {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] = ('a' + (b[i] - 'A'))
		}
	}
}

func (i item) String() string {
	var v string

	switch i._type {
	case itemEOF:
		v = "EOF"
	case itemError:
		v = "error"
	case itemName:
		v = "name"
	case itemPunctuator:
		v = "punctuator"
	case itemDirective:
		v = "directive"
	case itemVariable:
		v = "variable"
	case itemNumberVal:
		v = "number"
	case itemStringVal:
		v = "string"
	}
	return v
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
