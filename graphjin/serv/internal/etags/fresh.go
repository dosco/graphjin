package etags

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-http-utils/headers"
)

// IsFresh check whether cache can be used in this HTTP request
func IsFresh(reqHeader http.Header, resHeader http.Header) bool {
	isEtagMatched, isModifiedMatched := false, false
	// cacheControl := reqHeader.Get(headers.CacheControl)

	etag := resHeader.Get(headers.ETag)
	lastModified := resHeader.Get(headers.LastModified)

	// if strings.Contains(cacheControl, "no-cache") {
	// 	return false
	// }

	ifNoneMatch := reqHeader.Get(headers.IfNoneMatch)

	if etag != "" && ifNoneMatch != "" {
		isEtagMatched = checkEtagNoneMatch(trimTags(strings.Split(ifNoneMatch, ",")), etag)
	}

	ifMatch := reqHeader.Get(headers.IfMatch)

	if etag != "" && ifMatch != "" && !isEtagMatched {
		isEtagMatched = checkEtagMatch(trimTags(strings.Split(ifMatch, ",")), etag)
	}

	ifModifiedSince := reqHeader.Get(headers.IfModifiedSince)

	if lastModified != "" && ifModifiedSince != "" {
		isModifiedMatched = checkModifedMatch(lastModified, ifModifiedSince)
	}

	ifUnmodifiedSince := reqHeader.Get(headers.IfUnmodifiedSince)

	if lastModified != "" && ifUnmodifiedSince != "" && !isModifiedMatched {
		isModifiedMatched = checkUnmodifedMatch(lastModified, ifUnmodifiedSince)
	}

	return isEtagMatched || isModifiedMatched
}

func trimTags(tags []string) []string {
	trimedTags := make([]string, len(tags))

	for i, tag := range tags {
		trimedTags[i] = strings.TrimSpace(tag)
	}

	return trimedTags
}

func checkEtagNoneMatch(etagsToNoneMatch []string, etag string) bool {
	for _, etagToNoneMatch := range etagsToNoneMatch {
		if etagToNoneMatch == "*" || etagToNoneMatch == etag || etagToNoneMatch == "W/"+etag {
			return true
		}
	}

	return false
}

func checkEtagMatch(etagsToMatch []string, etag string) bool {
	for _, etagToMatch := range etagsToMatch {
		if etagToMatch == "*" {
			return false
		}

		if strings.HasPrefix(etagToMatch, "W/") {
			if etagToMatch == "W/"+etag {
				return false
			}
		} else {
			if etagToMatch == etag {
				return false
			}
		}
	}

	return true
}

func checkModifedMatch(lastModified, ifModifiedSince string) bool {
	if lm, ims, ok := parseTimePairs(lastModified, ifModifiedSince); ok {
		return lm.Before(ims)
	}

	return false
}

func checkUnmodifedMatch(lastModified, ifUnmodifiedSince string) bool {
	if lm, ius, ok := parseTimePairs(lastModified, ifUnmodifiedSince); ok {
		return lm.After(ius)
	}

	return false
}

func parseTimePairs(s1, s2 string) (t1 time.Time, t2 time.Time, ok bool) {
	if t1, err := time.Parse(http.TimeFormat, s1); err == nil {
		if t2, err := time.Parse(http.TimeFormat, s2); err == nil {
			return t1, t2, true
		}
	}

	return t1, t2, false
}
