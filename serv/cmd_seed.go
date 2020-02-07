package serv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path"

	"github.com/brianvoe/gofakeit"
	"github.com/dop251/goja"
	"github.com/spf13/cobra"
	"github.com/valyala/fasttemplate"
)

func cmdDBSeed(cmd *cobra.Command, args []string) {
	var err error

	if conf, err = initConf(); err != nil {
		errlog.Fatal().Err(err).Msg("failed to read config")
	}

	conf.Production = false

	db, err = initDBPool(conf)
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to connect to database")
	}

	initCompiler()

	sfile := path.Join(confPath, conf.SeedFile)

	b, err := ioutil.ReadFile(path.Join(confPath, conf.SeedFile))
	if err != nil {
		errlog.Fatal().Err(err).Msgf("failed to read seed file '%s'", sfile)
	}

	vm := goja.New()
	vm.Set("graphql", graphQLFunc)

	console := vm.NewObject()
	console.Set("log", logFunc) //nolint: errcheck
	vm.Set("console", console)

	fake := vm.NewObject()
	setFakeFuncs(fake)
	vm.Set("fake", fake)

	_, err = vm.RunScript("seed.js", string(b))
	if err != nil {
		errlog.Fatal().Err(err).Msg("failed to execute script")
	}

	logger.Info().Msg("seed script done")
}

//func runFunc(call goja.FunctionCall) {
func graphQLFunc(query string, data interface{}, opt map[string]string) map[string]interface{} {
	vars, err := json.Marshal(data)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	c := context.Background()

	if v, ok := opt["user_id"]; ok && len(v) != 0 {
		c = context.WithValue(c, userIDKey, v)
	}

	var role string

	if v, ok := opt["role"]; ok && len(v) != 0 {
		role = v
	} else {
		role = "user"
	}

	stmts, err := buildRoleStmt([]byte(query), vars, role)
	if err != nil {
		errlog.Fatal().Err(err).Msg("graphql query failed")
	}
	st := stmts[0]

	buf := &bytes.Buffer{}

	t := fasttemplate.New(st.sql, openVar, closeVar)
	_, err = t.ExecuteFunc(buf, argMap(c, vars))

	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	finalSQL := buf.String()

	tx, err := db.Begin(c)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}
	defer tx.Rollback(c) //nolint: errcheck

	if conf.DB.SetUserID {
		if err := setLocalUserID(c, tx); err != nil {
			errlog.Fatal().Err(err).Send()
		}
	}

	var root []byte

	if err = tx.QueryRow(context.Background(), finalSQL).Scan(&root); err != nil {
		errlog.Fatal().Err(err).Msg("sql query failed")
	}

	if err := tx.Commit(c); err != nil {
		errlog.Fatal().Err(err).Send()
	}

	val := make(map[string]interface{})

	err = json.Unmarshal(root, &val)
	if err != nil {
		errlog.Fatal().Err(err).Send()
	}

	return val
}

//nolint: errcheck
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
	return fmt.Sprintf("https://i.pravatar.cc/%d?%d", size, rand.Intn(5000))
}

func imageURL(width int, height int) string {
	return fmt.Sprintf("https://picsum.photos/%d/%d?%d", width, height, rand.Intn(5000))
}

//nolint: errcheck
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
	f.Set("status_code", gofakeit.StatusCode)
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
	f.Set("vehicle", gofakeit.Vehicle)
	f.Set("vehicle_type", gofakeit.VehicleType)
	f.Set("car_maker", gofakeit.CarMaker)
	f.Set("car_model", gofakeit.CarModel)
	f.Set("fuel_type", gofakeit.FuelType)
	f.Set("transmission_gear_type", gofakeit.TransmissionGearType)

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
	f.Set("simple_status_code", gofakeit.SimpleStatusCode)
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
	f.Set("credit_card_number_luhn", gofakeit.CreditCardNumberLuhn)
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
	f.Set("hacker_ingverb", gofakeit.HackerIngverb)
	f.Set("hacker_noun", gofakeit.HackerNoun)
	f.Set("hacker_phrase", gofakeit.HackerPhrase)
	f.Set("hacker_verb", gofakeit.HackerVerb)

	//Hipster
	f.Set("hipster_word", gofakeit.HipsterWord)
	f.Set("hipster_paragraph", gofakeit.HipsterParagraph)
	f.Set("hipster_sentence", gofakeit.HipsterSentence)

	//Languages
	//f.Set("language", gofakeit.Language)
	//f.Set("language_abbreviation", gofakeit.LanguageAbbreviation)
	//f.Set("language_abbreviation", gofakeit.LanguageAbbreviation)

	// File
	f.Set("extension", gofakeit.Extension)
	f.Set("mine_type", gofakeit.MimeType)

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
	f.Set("rand_string", gofakeit.RandString)
	f.Set("shuffle_strings", gofakeit.ShuffleStrings)
	f.Set("numerify", gofakeit.Numerify)

	//f.Set("programming_language", gofakeit.ProgrammingLanguage)

}
