package internal

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
)

var (
	ErrParseStrToLevel = errors.New("string can't be parsed to level, use: `error`, `info`, `debug`")
)

type Level int

const (
	ERR Level = iota
	INF
	DBG
)

func (l Level) String() string { return [4]string{"Error", "Info", "Debug"}[l] }

func NewStdLog(opts ...Option) *StdLog {
	l := &StdLog{
		fatal: log.New(os.Stderr, "\033[31mFATAL\033[0m: ", log.Ldate|log.Ltime),
		err:   log.New(os.Stderr, "\033[31mERR\033[0m: ", log.Ldate|log.Ltime),
		inf:   log.New(os.Stderr, "\033[32mINF\033[0m: ", log.Ldate|log.Ltime),
		dbg:   log.New(os.Stderr, "\033[35mDBG\033[0m: ", log.Ldate|log.Ltime),
		lvl:   INF,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

type StdLog struct {
	fatal, err, inf, dbg *log.Logger
	lvl                  Level
}

func (l *StdLog) Debug(format string, v ...interface{}) {
	if l.lvl < DBG {
		return
	}
	l.dbg.Printf(format, v...)
}

func (l *StdLog) Info(format string, v ...interface{}) {
	if l.lvl < INF {
		return
	}
	l.inf.Printf(format, v...)
}

func (l *StdLog) Error(format string, v ...interface{}) {
	if l.lvl < ERR {
		return
	}
	l.err.Printf(format, v...)
}

func (l *StdLog) Fatal(format string, v ...interface{}) {
	l.fatal.Printf(format, v...)
	os.Exit(1)
}

type Option func(l *StdLog)

func WithLevel(level Level) Option { return func(l *StdLog) { l.lvl = level } }

func ParseLevel(lvl string) (Level, error) {
	levels := map[string]Level{
		strings.ToLower(ERR.String()): ERR,
		strings.ToLower(INF.String()): INF,
		strings.ToLower(DBG.String()): DBG,
	}
	level, ok := levels[strings.ToLower(lvl)]
	if !ok {
		return INF, fmt.Errorf("%s %w", lvl, ErrParseStrToLevel)
	}
	return level, nil
}
