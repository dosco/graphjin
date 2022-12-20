package valid

import "regexp"

const (
	alphaRegexString               = "^[a-zA-Z]+$"
	alphaNumericRegexString        = "^[a-zA-Z0-9]+$"
	alphaUnicodeRegexString        = "^[\\p{L}]+$"
	alphaUnicodeNumericRegexString = "^[\\p{L}\\p{N}]+$"
	numericRegexString             = "^[-+]?[0-9]+(?:\\.[0-9]+)?$"
	numberRegexString              = "^[0-9]+$"
	emailRegexString               = "^(?:(?:(?:(?:[a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+(?:\\.([a-zA-Z]|\\d|[!#\\$%&'\\*\\+\\-\\/=\\?\\^_`{\\|}~]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])+)*)|(?:(?:\\x22)(?:(?:(?:(?:\\x20|\\x09)*(?:\\x0d\\x0a))?(?:\\x20|\\x09)+)?(?:(?:[\\x01-\\x08\\x0b\\x0c\\x0e-\\x1f\\x7f]|\\x21|[\\x23-\\x5b]|[\\x5d-\\x7e]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[\\x01-\\x09\\x0b\\x0c\\x0d-\\x7f]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}]))))*(?:(?:(?:\\x20|\\x09)*(?:\\x0d\\x0a))?(\\x20|\\x09)+)?(?:\\x22))))@(?:(?:(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])(?:[a-zA-Z]|\\d|-|\\.|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*(?:[a-zA-Z]|\\d|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.)+(?:(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])|(?:(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])(?:[a-zA-Z]|\\d|-|\\.|~|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])*(?:[a-zA-Z]|[\\x{00A0}-\\x{D7FF}\\x{F900}-\\x{FDCF}\\x{FDF0}-\\x{FFEF}])))\\.?$"
	uUID3RegexString               = "^[0-9a-f]{8}-[0-9a-f]{4}-3[0-9a-f]{3}-[0-9a-f]{4}-[0-9a-f]{12}$"
	uUID4RegexString               = "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
	uUID5RegexString               = "^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$"
	uUIDRegexString                = "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	uUID3RFC4122RegexString        = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-3[0-9a-fA-F]{3}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	uUID4RFC4122RegexString        = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$"
	uUID5RFC4122RegexString        = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-5[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$"
	uUIDRFC4122RegexString         = "^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	uLIDRegexString                = "^[A-HJKMNP-TV-Z0-9]{26}$"
	aSCIIRegexString               = "^[\x00-\x7F]*$"
	printableASCIIRegexString      = "^[\x20-\x7E]*$"
	multibyteRegexString           = "[^\x00-\x7F]"
	dataURIRegexString             = `^data:((?:\w+\/(?:([^;]|;[^;]).)+)?)`
	uRLEncodedRegexString          = `^(?:[^%]|%[0-9A-Fa-f]{2})*$`
	hTMLEncodedRegexString         = `&#[x]?([0-9a-fA-F]{2})|(&gt)|(&lt)|(&quot)|(&amp)+[;]?`
	hTMLRegexString                = `<[/]?([a-zA-Z]+).*?>`
	jWTRegexString                 = "^[A-Za-z0-9-_]+\\.[A-Za-z0-9-_]+\\.[A-Za-z0-9-_]*$"
	splitParamsRegexString         = `'[^']*'|\S+`
	bicRegexString                 = `^[A-Za-z]{6}[A-Za-z0-9]{2}([A-Za-z0-9]{3})?$`
	semverRegexString              = `^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$` // numbered capture groups https://semver.org/
	dnsRegexStringRFC1035Label     = "^[a-z]([-a-z0-9]*[a-z0-9]){0,62}$"
)

var (
	alphaRegex               = regexp.MustCompile(alphaRegexString)
	alphaNumericRegex        = regexp.MustCompile(alphaNumericRegexString)
	alphaUnicodeRegex        = regexp.MustCompile(alphaUnicodeRegexString)
	alphaUnicodeNumericRegex = regexp.MustCompile(alphaUnicodeNumericRegexString)
	numericRegex             = regexp.MustCompile(numericRegexString)
	numberRegex              = regexp.MustCompile(numberRegexString)
	emailRegex               = regexp.MustCompile(emailRegexString)
	uUID3Regex               = regexp.MustCompile(uUID3RegexString)
	uUID4Regex               = regexp.MustCompile(uUID4RegexString)
	uUID5Regex               = regexp.MustCompile(uUID5RegexString)
	uUIDRegex                = regexp.MustCompile(uUIDRegexString)
	uUID3RFC4122Regex        = regexp.MustCompile(uUID3RFC4122RegexString)
	uUID4RFC4122Regex        = regexp.MustCompile(uUID4RFC4122RegexString)
	uUID5RFC4122Regex        = regexp.MustCompile(uUID5RFC4122RegexString)
	uUIDRFC4122Regex         = regexp.MustCompile(uUIDRFC4122RegexString)
	uLIDRegex                = regexp.MustCompile(uLIDRegexString)
	aSCIIRegex               = regexp.MustCompile(aSCIIRegexString)
	printableASCIIRegex      = regexp.MustCompile(printableASCIIRegexString)
	multibyteRegex           = regexp.MustCompile(multibyteRegexString)
	uRLEncodedRegex          = regexp.MustCompile(uRLEncodedRegexString)
	splitParamsRegex         = regexp.MustCompile(splitParamsRegexString)
)
