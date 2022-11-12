package serv

// import (
// 	"context"

// 	"github.com/jackc/pgx/v5"
// 	"github.com/rs/zerolog"
// )

// type Logger struct {
// 	logger zerolog.Logger
// }

// // NewLogger accepts a zerolog.Logger as input and returns a new custom pgx
// // logging fascade as output.
// func NewSQLLogger(logger zerolog.Logger) *Logger {
// 	return &Logger{
// 		logger: // logger.With().Logger(),
// 	}
// }

// func (pl *Logger) Log(ctx context.Context, level pgx.LogLevel, msg string, data map[string]interface{}) {
// 	var zlevel zerolog.Level
// 	switch level {
// 	case pgx.LogLevelNone:
// 		zlevel = zerolog.NoLevel
// 	case pgx.LogLevelError:
// 		zlevel = zerolog.ErrorLevel
// 	case pgx.LogLevelWarn:
// 		zlevel = zerolog.WarnLevel
// 	case pgx.LogLevelDebug, pgx.LogLevelInfo:
// 		zlevel = zerolog.DebugLevel
// 	default:
// 		zlevel = zerolog.DebugLevel
// 	}

// 	if sql, ok := data["sql"]; ok {
// 		delete(data, "sql")
// 		pl.// logger.WithLevel(zlevel).Fields(data).Msg(sql.(string))
// 	} else {
// 		pl.// logger.WithLevel(zlevel).Fields(data).Msg(msg)
// 	}
// }
