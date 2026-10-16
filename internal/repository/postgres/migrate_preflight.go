package postgres

import (
	"errors"
	"fmt"
	"strings"
)

// findTransactionControl returns the first top-level transaction-control
// statement of a migration file and its 1-based line, or "" when there is
// none. The migrator runs each file inside its own transaction, and the
// simple query protocol executes a top-level COMMIT as soon as it reaches it,
// so such a file is rejected before any of it is sent.
//
// Statements are split the way PostgreSQL lexes and psql splits a script:
// -- and nested /* */ comments, '...' strings (a doubled quote escapes; E'...' with
// backslash escapes; standard_conforming_strings must be on), "..."
// identifiers, $tag$...$tag$ bodies, identifiers that contain $, semicolons
// inside parentheses (CREATE RULE ... DO (a; b)) and psql's BEGIN/CASE/END
// depth for CREATE FUNCTION/PROCEDURE ... BEGIN ATOMIC bodies. Only the
// leading keyword of each statement is examined, so these words inside
// comments, literals, bodies and identifiers are never matches. An
// unterminated comment, literal or body is an error.
func findTransactionControl(sql string) (string, int, error) {
	var (
		words      []string // leading unquoted words of the statement, lowercased
		firstWord  bool     // the statement's first token is an unquoted word
		started    bool
		startPos   int
		parenDepth int
		beginDepth int
	)
	finish := func() string {
		statement := ""
		if firstWord && len(words) > 0 {
			switch words[0] {
			case "begin", "start", "commit", "end", "rollback", "abort", "savepoint", "release":
				statement = strings.ToUpper(words[0])
			case "prepare":
				if len(words) > 1 && words[1] == "transaction" {
					statement = "PREPARE TRANSACTION"
				}
			}
		}
		words, firstWord, started, parenDepth, beginDepth = nil, false, false, 0, 0
		return statement
	}
	token := func(pos int, word bool) {
		if !started {
			started, startPos, firstWord = true, pos, word
		}
	}
	line := func(pos int) int { return strings.Count(sql[:pos], "\n") + 1 }

	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == '-' && strings.HasPrefix(sql[i:], "--"):
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				i = len(sql)
			} else {
				i += end + 1
			}
		case c == '/' && strings.HasPrefix(sql[i:], "/*"):
			end, err := skipBlockComment(sql, i)
			if err != nil {
				return "", 0, fmt.Errorf("line %d: %w", line(i), err)
			}
			i = end
		case c == '\'':
			token(i, false)
			end, err := skipQuoted(sql, i, '\'', false)
			if err != nil {
				return "", 0, fmt.Errorf("line %d: %w", line(i), err)
			}
			i = end
		case c == '"':
			token(i, false)
			end, err := skipQuoted(sql, i, '"', false)
			if err != nil {
				return "", 0, fmt.Errorf("line %d: %w", line(i), err)
			}
			i = end
		case c == '$':
			token(i, false)
			if tag, ok := dollarTag(sql, i); ok {
				end := strings.Index(sql[i+len(tag):], tag)
				if end < 0 {
					return "", 0, fmt.Errorf("line %d: unterminated dollar-quoted string %s", line(i), tag)
				}
				i += len(tag) + end + len(tag)
			} else {
				i++ // a positional parameter such as $1
			}
		case identifierStart(c):
			start := i
			for i < len(sql) && (identifierStart(sql[i]) || isDigit(sql[i]) || sql[i] == '$') {
				i++
			}
			word := strings.ToLower(sql[start:i])
			// E'...' and U&'...' / U&"..." are literals, not words.
			if word == "e" && i < len(sql) && sql[i] == '\'' {
				token(start, false)
				end, err := skipQuoted(sql, i, '\'', true)
				if err != nil {
					return "", 0, fmt.Errorf("line %d: %w", line(start), err)
				}
				i = end
				continue
			}
			if word == "u" && (strings.HasPrefix(sql[i:], "&'") || strings.HasPrefix(sql[i:], "&\"")) {
				token(start, false)
				end, err := skipQuoted(sql, i+1, sql[i+1], false)
				if err != nil {
					return "", 0, fmt.Errorf("line %d: %w", line(start), err)
				}
				i = end
				continue
			}
			token(start, true)
			if len(words) < 4 {
				words = append(words, word)
			}
			switch {
			case word == "begin" && createsRoutine(words):
				beginDepth++
			case word == "case" && beginDepth > 0:
				beginDepth++
			case word == "end" && beginDepth > 0:
				beginDepth--
			}
		case isDigit(c):
			token(i, false)
			for i < len(sql) && (identifierStart(sql[i]) || isDigit(sql[i]) || sql[i] == '.') {
				i++
			}
		case c == '(':
			token(i, false)
			parenDepth++
			i++
		case c == ')':
			token(i, false)
			parenDepth = max(0, parenDepth-1)
			i++
		case c == ';':
			if parenDepth == 0 && beginDepth == 0 {
				pos := startPos
				if statement := finish(); statement != "" {
					return statement, line(pos), nil
				}
			}
			i++
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		default:
			token(i, false)
			i++
		}
	}
	// Unbalanced depth would hide later semicolons; refuse rather than guess.
	if beginDepth > 0 {
		return "", 0, fmt.Errorf("line %d: unterminated BEGIN ATOMIC body", line(startPos))
	}
	if parenDepth > 0 {
		return "", 0, fmt.Errorf("line %d: unbalanced parentheses", line(startPos))
	}
	pos := startPos
	if statement := finish(); statement != "" {
		return statement, line(pos), nil
	}
	return "", 0, nil
}

// createsRoutine mirrors psql: BEGIN opens an atomic body only in CREATE
// [OR REPLACE] FUNCTION/PROCEDURE statements. words holds the statement's
// leading words.
func createsRoutine(words []string) bool {
	if len(words) < 3 || words[0] != "create" {
		return false
	}
	if words[1] == "function" || words[1] == "procedure" {
		return true
	}
	return len(words) >= 4 && words[1] == "or" && words[2] == "replace" && (words[3] == "function" || words[3] == "procedure")
}

func identifierStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// skipQuoted returns the index after the literal opened at sql[start]. A
// doubled quote is an escaped quote; backslash escapes apply to E'...' only.
func skipQuoted(sql string, start int, quote byte, backslash bool) (int, error) {
	for i := start + 1; i < len(sql); i++ {
		switch {
		case backslash && sql[i] == '\\':
			i++
		case sql[i] == quote:
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}
			return i + 1, nil
		}
	}
	if quote == '"' {
		return 0, errors.New("unterminated quoted identifier")
	}
	return 0, errors.New("unterminated string literal")
}

// skipBlockComment returns the index after a /* */ comment; they nest.
func skipBlockComment(sql string, start int) (int, error) {
	depth := 0
	for i := start; i < len(sql)-1; i++ {
		switch {
		case sql[i] == '/' && sql[i+1] == '*':
			depth++
			i++
		case sql[i] == '*' && sql[i+1] == '/':
			depth--
			i++
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, errors.New("unterminated block comment")
}

// dollarTag returns the opening delimiter ($$ or $tag$) at sql[start]. A tag
// follows identifier rules without $ and cannot start with a digit.
func dollarTag(sql string, start int) (string, bool) {
	i := start + 1
	if i < len(sql) && isDigit(sql[i]) {
		return "", false
	}
	for i < len(sql) && (identifierStart(sql[i]) || isDigit(sql[i])) {
		i++
	}
	if i < len(sql) && sql[i] == '$' {
		return sql[start : i+1], true
	}
	return "", false
}
