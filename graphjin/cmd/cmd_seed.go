package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/dop251/goja"
	"github.com/dosco/graphjin/core/v3"
	"github.com/gosimple/slug"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	babel "github.com/jvatic/goja-babel"
	"github.com/spf13/cobra"
)

func cmdDBSeed(cmd *cobra.Command, args []string) {
	setup(cpath)
	initDB(true)

	if conf.DB.Type == "mysql" {
		log.Fatalf("Seed scripts not support with MySQL")
	}

	conf.Serv.Production = false
	conf.DefaultBlock = false
	conf.DisableAllowList = true
	conf.DBSchemaPollDuration = -1

	conf.Core.Blocklist = nil
	seed := filepath.Join(cpath, "seed.js")

	log.Infof("Seed script started (please wait)")

	if err := compileAndRunJS(seed, db); err != nil {
		log.Fatalf("Failed to execute seed file %s: %s", seed, err)
	}

	log.Infof("Seed script completed")
}

func compileAndRunJS(seed string, db *sql.DB) error {
	b, err := os.ReadFile(seed)
	if err != nil {
		return fmt.Errorf("Failed to read seed file %s: %s", seed, err)
	}

	gj, err := core.NewGraphJin(&conf.Core, db)
	if err != nil {
		return err
	}

	graphQLFn := func(query string, data interface{}, opt map[string]string) map[string]interface{} {
		return graphQLFunc(gj, query, data, opt)
	}

	importCSVFn := func(table, filename string, sep string) int64 {
		return importCSV(table, filename, sep, db)
	}

	vm := goja.New()
	if err := vm.Set("graphql", graphQLFn); err != nil {
		return err
	}

	if err := vm.Set("import_csv", importCSVFn); err != nil {
		return err
	}

	console := vm.NewObject()
	console.Set("log", logFunc) //nolint:errcheck
	if err := vm.Set("console", console); err != nil {
		return err
	}

	fake := vm.NewObject()
	setFakeFuncs(fake)
	if err := vm.Set("fake", fake); err != nil {
		return err
	}

	util := vm.NewObject()
	setUtilFuncs(util)
	if err := vm.Set("util", util); err != nil {
		return err
	}

	babelOptions := map[string]interface{}{
		"plugins": []string{
			"proposal-async-generator-functions",
			"proposal-class-properties",
			"proposal-dynamic-import",
			"proposal-json-strings",
			"proposal-nullish-coalescing-operator",
			"proposal-numeric-separator",
			"proposal-object-rest-spread",
			"proposal-optional-catch-binding",
			"proposal-optional-chaining",
			"proposal-private-methods",
			"proposal-unicode-property-regex",
			"syntax-async-generators",
			"syntax-class-properties",
			// "syntax-dynamic-import",
			// "syntax-json-strings",
			// "syntax-nullish-coalescing-operator",
			// "syntax-numeric-separator",
			"syntax-object-rest-spread",
			"syntax-optional-catch-binding",
			// "syntax-optional-chaining",
			"syntax-top-level-await",
			"transform-arrow-functions",
			"transform-async-to-generator",
			"transform-block-scoped-functions",
			"transform-block-scoping",
			"transform-classes",
			"transform-computed-properties",
			"transform-destructuring",
			"transform-dotall-regex",
			"transform-duplicate-keys",
			"transform-exponentiation-operator",
			"transform-for-of",
			"transform-function-name",
			"transform-literals",
			"transform-member-expression-literals",
			// "transform-modules-amd",
			"transform-modules-commonjs",
			// "transform-modules-systemjs",
			// "transform-modules-umd",
			"transform-named-capturing-groups-regex",
			"transform-new-target",
			"transform-object-super",
			"transform-parameters",
			"transform-property-literals",
			"transform-regenerator",
			"transform-reserved-words",
			"transform-shorthand-properties",
			"transform-spread",
			"transform-sticky-regex",
			"transform-template-literals",
			"transform-typeof-symbol",
			"transform-unicode-escapes",
			"transform-unicode-regex",
		},

		"retainLines": true,
	}

	es5, err := babel.Transform(bytes.NewReader(b), babelOptions)
	if err != nil {
		return err
	}

	es5Code := new(strings.Builder)
	if _, err := io.Copy(es5Code, es5); err != nil {
		return err
	}

	_, err = vm.RunScript("seed.js", es5Code.String())
	return err
}

// func runFunc(call goja.FunctionCall) {
func graphQLFunc(gj *core.GraphJin, query string, data interface{}, opt map[string]string) map[string]interface{} {
	ct := context.Background()

	if v, ok := opt["user_id"]; ok && v != "" {
		ct = context.WithValue(ct, core.UserIDKey, v)
	}

	// var role string

	// if v, ok := opt["role"]; ok && len(v) != 0 {
	// 	role = v
	// } else {
	// 	role = "user"
	// }

	if conf.Debug {
		log.Debugf("Seed query: %s", query)
	}

	var vars []byte
	var err error

	if vars, err = json.Marshal(data); err != nil {
		log.Fatalf("Failed parsing seed query variables: %s", err)
	}

	res, err := gj.GraphQL(ct, query, vars, nil)
	if err != nil {
		log.Fatalf("Seed query failed: %s", err)
	}

	val := make(map[string]interface{})

	if err = json.Unmarshal(res.Data, &val); err != nil {
		log.Fatalf("Seed query failed: %s", err)
	}

	return val
}

type csvSource struct {
	rows [][]string
	i    int
}

func NewCSVSource(filename string, sep rune) (*csvSource, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if sep == 0 {
		sep = ','
	}

	r := csv.NewReader(f)
	r.ReuseRecord = true
	r.Comma = sep

	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	return &csvSource{rows: rows}, nil
}

func (c *csvSource) Next() bool {
	return c.i < len(c.rows)
}

func (c *csvSource) Values() ([]interface{}, error) {
	var vals []interface{}
	var err error

	for _, v := range c.rows[c.i] {
		switch {
		case v == "":
			vals = append(vals, "")
		case isDigit(v):
			var n int
			if n, err = strconv.Atoi(v); err == nil {
				vals = append(vals, n)
			}
		case strings.EqualFold(v, "true") || strings.EqualFold(v, "false"):
			var b bool
			if b, err = strconv.ParseBool(v); err == nil {
				vals = append(vals, b)
			}
		default:
			vals = append(vals, v)
		}

		if err != nil {
			return nil, fmt.Errorf("%w (line no %d)", err, c.i)
		}
	}
	c.i++

	return vals, nil
}

func isDigit(v string) bool {
	for i := range v {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func (c *csvSource) Err() error {
	return nil
}

func importCSV(table, filename string, sep string, db *sql.DB) int64 {
	log.Infof("Seeding table: %s, From file: %s", table, filename)

	if filename[0] != '/' && filename[0] != '.' {
		filename = filepath.Join(cpath, filename)
	}

	var sepRune rune

	if sep != "" {
		sepRune = rune(sep[0])
	}

	s, err := NewCSVSource(filename, sepRune)
	if err != nil {
		log.Fatalf("Error reading CSV file: %s", err)
	}

	var cols []string
	colval, _ := s.Values()

	for _, c := range colval {
		cols = append(cols, c.(string))
	}

	c := context.Background()
	conn, err := db.Conn(c)
	if err != nil {
		log.Fatalf("Error connecting to database: %s", err)
	}
	//nolint:errcheck
	defer conn.Close()

	var n int64

	err = conn.Raw(func(driverConn interface{}) error {
		conn := driverConn.(*stdlib.Conn).Conn()
		n, err = conn.CopyFrom(c,
			pgx.Identifier{table},
			cols,
			s)

		if err != nil {
			err = fmt.Errorf("%w (line no %d)", err, s.i)
			log.Fatalf("Error with copy-from: %s", err)
		}
		return nil
	})

	return n
}

// nolint:errcheck
func logFunc(args ...interface{}) {
	for _, arg := range args {
		if _, ok := arg.(map[string]interface{}); ok {
			j, err := json.MarshalIndent(arg, "", "  ")
			if err != nil {
				continue
			}
			os.Stdout.Write(j)
		} else {
			io.WriteString(os.Stdout, fmt.Sprintf("%v", arg))
		}

		io.WriteString(os.Stdout, "\n")
	}
}

func avatarURL(size int) string {
	if size == 0 {
		size = 200
	}
	// #nosec G404
	return fmt.Sprintf("https://i.pravatar.cc/%d?%d", size, rand.Intn(5000))
}

func imageURL(width, height int) string {
	// #nosec G404
	return fmt.Sprintf("https://picsum.photos/%d/%d?%d", width, height, rand.Intn(5000))
}

func getRandValue(values []string) string {
	// #nosec G404
	return values[rand.Intn(len(values))]
}

// nolint:errcheck
func setFakeFuncs(f *goja.Object) {
	gofakeit.Seed(0)

	// Person
	f.Set("person", gofakeit.Person)
	f.Set("name", gofakeit.Name)
	f.Set("name_prefix", gofakeit.NamePrefix)
	f.Set("name_suffix", gofakeit.NameSuffix)
	f.Set("first_name", gofakeit.FirstName)
	f.Set("last_name", gofakeit.LastName)
	f.Set("gender", gofakeit.Gender)
	f.Set("ssn", gofakeit.SSN)
	f.Set("contact", gofakeit.Contact)
	f.Set("email", gofakeit.Email)
	f.Set("phone", gofakeit.Phone)
	f.Set("phone_formatted", gofakeit.PhoneFormatted)
	f.Set("username", gofakeit.Username)
	f.Set("password", gofakeit.Password)

	// Address
	f.Set("address", gofakeit.Address)
	f.Set("city", gofakeit.City)
	f.Set("country", gofakeit.Country)
	f.Set("country_abr", gofakeit.CountryAbr)
	f.Set("state", gofakeit.State)
	f.Set("state_abr", gofakeit.StateAbr)
	f.Set("street", gofakeit.Street)
	f.Set("street_name", gofakeit.StreetName)
	f.Set("street_number", gofakeit.StreetNumber)
	f.Set("street_prefix", gofakeit.StreetPrefix)
	f.Set("street_suffix", gofakeit.StreetSuffix)
	f.Set("zip", gofakeit.Zip)
	f.Set("latitude", gofakeit.Latitude)
	f.Set("latitude_in_range", gofakeit.LatitudeInRange)
	f.Set("longitude", gofakeit.Longitude)
	f.Set("longitude_in_range", gofakeit.LongitudeInRange)

	// Beer
	f.Set("beer_alcohol", gofakeit.BeerAlcohol)
	f.Set("beer_hop", gofakeit.BeerHop)
	f.Set("beer_ibu", gofakeit.BeerIbu)
	f.Set("beer_blg", gofakeit.BeerBlg)
	f.Set("beer_malt", gofakeit.BeerMalt)
	f.Set("beer_name", gofakeit.BeerName)
	f.Set("beer_style", gofakeit.BeerStyle)
	f.Set("beer_yeast", gofakeit.BeerYeast)

	// Cars
	f.Set("car", gofakeit.Car)
	f.Set("car_type", gofakeit.CarType)
	f.Set("car_maker", gofakeit.CarMaker)
	f.Set("car_model", gofakeit.CarModel)

	// Text
	f.Set("word", gofakeit.Word)
	f.Set("sentence", gofakeit.Sentence)
	f.Set("paragraph", gofakeit.Paragraph)
	f.Set("question", gofakeit.Question)
	f.Set("quote", gofakeit.Quote)

	// Misc
	f.Set("generate", gofakeit.Generate)
	f.Set("boolean", gofakeit.Bool)
	f.Set("uuid", gofakeit.UUID)

	// Colors
	f.Set("color", gofakeit.Color)
	f.Set("hex_color", gofakeit.HexColor)
	f.Set("rgb_color", gofakeit.RGBColor)
	f.Set("safe_color", gofakeit.SafeColor)

	// Internet
	f.Set("url", gofakeit.URL)
	f.Set("image_url", imageURL)
	f.Set("avatar_url", avatarURL)
	f.Set("domain_name", gofakeit.DomainName)
	f.Set("domain_suffix", gofakeit.DomainSuffix)
	f.Set("ipv4_address", gofakeit.IPv4Address)
	f.Set("ipv6_address", gofakeit.IPv6Address)
	f.Set("http_method", gofakeit.HTTPMethod)
	f.Set("user_agent", gofakeit.UserAgent)
	f.Set("user_agent_firefox", gofakeit.FirefoxUserAgent)
	f.Set("user_agent_chrome", gofakeit.ChromeUserAgent)
	f.Set("user_agent_opera", gofakeit.OperaUserAgent)
	f.Set("user_agent_safari", gofakeit.SafariUserAgent)

	// Date / Time
	f.Set("date", gofakeit.Date)
	f.Set("date_range", gofakeit.DateRange)
	f.Set("nano_second", gofakeit.NanoSecond)
	f.Set("second", gofakeit.Second)
	f.Set("minute", gofakeit.Minute)
	f.Set("hour", gofakeit.Hour)
	f.Set("month", gofakeit.Month)
	f.Set("day", gofakeit.Day)
	f.Set("weekday", gofakeit.WeekDay)
	f.Set("year", gofakeit.Year)
	f.Set("timezone", gofakeit.TimeZone)
	f.Set("timezone_abv", gofakeit.TimeZoneAbv)
	f.Set("timezone_full", gofakeit.TimeZoneFull)
	f.Set("timezone_offset", gofakeit.TimeZoneOffset)

	// Payment
	f.Set("price", gofakeit.Price)
	f.Set("credit_card", gofakeit.CreditCard)
	f.Set("credit_card_cvv", gofakeit.CreditCardCvv)
	f.Set("credit_card_number", gofakeit.CreditCardNumber)
	f.Set("credit_card_type", gofakeit.CreditCardType)
	f.Set("currency", gofakeit.Currency)
	f.Set("currency_long", gofakeit.CurrencyLong)
	f.Set("currency_short", gofakeit.CurrencyShort)

	// Company
	f.Set("bs", gofakeit.BS)
	f.Set("buzzword", gofakeit.BuzzWord)
	f.Set("company", gofakeit.Company)
	f.Set("company_suffix", gofakeit.CompanySuffix)
	f.Set("job", gofakeit.Job)
	f.Set("job_description", gofakeit.JobDescriptor)
	f.Set("job_level", gofakeit.JobLevel)
	f.Set("job_title", gofakeit.JobTitle)

	// Hacker
	f.Set("hacker_abbreviation", gofakeit.HackerAbbreviation)
	f.Set("hacker_adjective", gofakeit.HackerAdjective)
	f.Set("hacker_noun", gofakeit.HackerNoun)
	f.Set("hacker_phrase", gofakeit.HackerPhrase)
	f.Set("hacker_verb", gofakeit.HackerVerb)

	// Hipster
	f.Set("hipster_word", gofakeit.HipsterWord)
	f.Set("hipster_paragraph", gofakeit.HipsterParagraph)
	f.Set("hipster_sentence", gofakeit.HipsterSentence)

	// File
	f.Set("file_extension", gofakeit.FileExtension)
	f.Set("file_mine_type", gofakeit.FileMimeType)

	// Numbers
	f.Set("number", gofakeit.Number)
	f.Set("numerify", gofakeit.Numerify)
	f.Set("int8", gofakeit.Int8)
	f.Set("int16", gofakeit.Int16)
	f.Set("int32", gofakeit.Int32)
	f.Set("int64", gofakeit.Int64)
	f.Set("uint8", gofakeit.Uint8)
	f.Set("uint16", gofakeit.Uint16)
	f.Set("uint32", gofakeit.Uint32)
	f.Set("uint64", gofakeit.Uint64)
	f.Set("float32", gofakeit.Float32)
	f.Set("float32_range", gofakeit.Float32Range)
	f.Set("float64", gofakeit.Float64)
	f.Set("float64_range", gofakeit.Float64Range)
	f.Set("shuffle_ints", gofakeit.ShuffleInts)
	f.Set("mac_address", gofakeit.MacAddress)

	// String
	f.Set("digit", gofakeit.Digit)
	f.Set("letter", gofakeit.Letter)
	f.Set("lexify", gofakeit.Lexify)
	f.Set("rand_string", getRandValue)
	f.Set("numerify", gofakeit.Numerify)
}

// nolint:errcheck
func setUtilFuncs(f *goja.Object) {
	// Slugs
	f.Set("make_slug", slug.Make)
	f.Set("make_slug_lang", slug.MakeLang)
	f.Set("shuffle_strings", gofakeit.ShuffleStrings)
}
