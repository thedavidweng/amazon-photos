package aphotos

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type MediaTypeFilter string

const (
	MediaPhotosAndVideos MediaTypeFilter = "PHOTOS OR VIDEOS"
	MediaPhotos          MediaTypeFilter = "PHOTOS"
	MediaVideos          MediaTypeFilter = "VIDEOS"
)

type SortOrder string

const (
	SortCreatedDesc     SortOrder = "['createdDate DESC']"
	SortCreatedAsc      SortOrder = "['createdDate ASC']"
	SortContentDateDesc SortOrder = "['contentProperties.contentDate DESC']"
	SortContentDateAsc  SortOrder = "['contentProperties.contentDate ASC']"
	SortModifiedDesc    SortOrder = "['modifiedDate DESC']"
	SortModifiedAsc     SortOrder = "['modifiedDate ASC']"
	SortNameAsc         SortOrder = "['name ASC']"
	SortNameDesc        SortOrder = "['name DESC']"
)

type FilterBuilder struct {
	clauses []string
	err     error
}

func NewFilter() *FilterBuilder {
	return &FilterBuilder{}
}

func RawFilter(raw string) *FilterBuilder {
	fb := &FilterBuilder{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		fb.setErr("raw filter must not be empty")
		return fb
	}
	if hasControlChars(trimmed) {
		fb.setErr("raw filter contains invalid control characters")
		return fb
	}
	fb.clauses = append(fb.clauses, trimmed)
	return fb
}

func (b *FilterBuilder) setErr(msg string) {
	if b.err == nil {
		b.err = &InvalidFilterError{
			Message: msg,
		}
	}
}

func (b *FilterBuilder) MediaType(mt MediaTypeFilter) *FilterBuilder {
	switch mt {
	case MediaPhotosAndVideos, MediaPhotos, MediaVideos:
		b.clauses = append(b.clauses, fmt.Sprintf("type:(%s)", string(mt)))
	default:
		b.setErr(fmt.Sprintf("unsupported media type filter %q", mt))
	}
	return b
}

func (b *FilterBuilder) Kind(kind NodeKind) *FilterBuilder {
	switch kind {
	case NodeKindFile, NodeKindFolder, NodeKindVisualCollection:
		b.clauses = append(b.clauses, fmt.Sprintf("kind:%s", string(kind)))
	default:
		b.setErr(fmt.Sprintf("unsupported node kind filter %q", kind))
	}
	return b
}

func (b *FilterBuilder) Status(status NodeStatus) *FilterBuilder {
	switch status {
	case NodeStatusAvailable, NodeStatusTrash:
		b.clauses = append(b.clauses, fmt.Sprintf("status:%s", string(status)))
	default:
		b.setErr(fmt.Sprintf("unsupported node status filter %q", status))
	}
	return b
}

func (b *FilterBuilder) IsRoot(isRoot bool) *FilterBuilder {
	b.clauses = append(b.clauses, fmt.Sprintf("isRoot:%t", isRoot))
	return b
}

func (b *FilterBuilder) NameExact(name string) *FilterBuilder {
	val, err := escapeFilterValue(name)
	if err != nil {
		b.setErr("invalid name filter: " + err.Error())
		return b
	}
	b.clauses = append(b.clauses, "name:"+val)
	return b
}

func (b *FilterBuilder) NamePrefix(prefix string) *FilterBuilder {
	val, err := escapeFilterValue(prefix)
	if err != nil {
		b.setErr("invalid name prefix filter: " + err.Error())
		return b
	}
	b.clauses = append(b.clauses, "name:"+val+"*")
	return b
}

func (b *FilterBuilder) ParentID(parentID string) *FilterBuilder {
	val, err := escapeFilterValue(strings.TrimSpace(parentID))
	if err != nil {
		b.setErr("invalid parent ID filter: " + err.Error())
		return b
	}
	b.clauses = append(b.clauses, "parents:"+val)
	return b
}

func (b *FilterBuilder) ContentTypePrefix(prefix string) *FilterBuilder {
	trimmed := strings.TrimSuffix(strings.TrimSpace(prefix), "*")
	val, err := escapeFilterValue(trimmed)
	if err != nil {
		b.setErr("invalid contentType prefix filter: " + err.Error())
		return b
	}
	b.clauses = append(b.clauses, "contentProperties.contentType:"+val+"*")
	return b
}

func (b *FilterBuilder) MD5(md5Hash string) *FilterBuilder {
	trimmed := strings.ToLower(strings.TrimSpace(md5Hash))
	if len(trimmed) != 32 || !isHex(trimmed) {
		b.setErr(fmt.Sprintf("invalid MD5 filter %q: must be 32 hex characters", md5Hash))
		return b
	}
	b.clauses = append(b.clauses, "contentProperties.md5:"+trimmed)
	return b
}

func (b *FilterBuilder) Year(year int) *FilterBuilder {
	if year < 1900 || year > 2200 {
		b.setErr(fmt.Sprintf("invalid year filter %d: must be between 1900 and 2200", year))
		return b
	}
	b.clauses = append(b.clauses, fmt.Sprintf("timeYear:(%d)", year))
	return b
}

func (b *FilterBuilder) Month(month int) *FilterBuilder {
	if month < 1 || month > 12 {
		b.setErr(fmt.Sprintf("invalid month filter %d: must be between 1 and 12", month))
		return b
	}
	b.clauses = append(b.clauses, fmt.Sprintf("timeMonth:(%d)", month))
	return b
}

func (b *FilterBuilder) Day(day int) *FilterBuilder {
	if day < 1 || day > 31 {
		b.setErr(fmt.Sprintf("invalid day filter %d: must be between 1 and 31", day))
		return b
	}
	b.clauses = append(b.clauses, fmt.Sprintf("timeDay:(%d)", day))
	return b
}

func (b *FilterBuilder) DateRange(field string, low, high time.Time, inclusiveLow, inclusiveHigh bool) *FilterBuilder {
	cleanField := strings.TrimSpace(field)
	switch cleanField {
	case "createdDate", "modifiedDate", "contentProperties.contentDate":
	default:
		b.setErr(fmt.Sprintf("unsupported date range field %q", field))
		return b
	}

	if low.IsZero() && high.IsZero() {
		b.setErr("date range filter requires at least one non-zero bound")
		return b
	}
	if !low.IsZero() && !high.IsZero() {
		if low.After(high) {
			b.setErr("date range low bound cannot be after high bound")
			return b
		}
		if low.Equal(high) && (!inclusiveLow || !inclusiveHigh) {
			b.setErr("exclusive date range with equal bounds is empty")
			return b
		}
	}

	openBracket := "{"
	if inclusiveLow {
		openBracket = "["
	}
	closeBracket := "}"
	if inclusiveHigh {
		closeBracket = "]"
	}

	lowToken := "*"
	if !low.IsZero() {
		lowToken = `"` + low.UTC().Format(time.RFC3339) + `"`
	}
	highToken := "*"
	if !high.IsZero() {
		highToken = `"` + high.UTC().Format(time.RFC3339) + `"`
	}

	b.clauses = append(b.clauses, fmt.Sprintf("%s:%s%s TO %s%s", cleanField, openBracket, lowToken, highToken, closeBracket))
	return b
}

func (b *FilterBuilder) PeopleIDs(ids ...string) *FilterBuilder {
	expr, err := formatDisjunction("people", ids)
	if err != nil {
		b.setErr(err.Error())
		return b
	}
	b.clauses = append(b.clauses, expr)
	return b
}

func (b *FilterBuilder) ClusterIDs(ids ...string) *FilterBuilder {
	expr, err := formatDisjunction("clusterId", ids)
	if err != nil {
		b.setErr(err.Error())
		return b
	}
	b.clauses = append(b.clauses, expr)
	return b
}

func (b *FilterBuilder) Locations(locs ...string) *FilterBuilder {
	expr, err := formatDisjunction("location", locs)
	if err != nil {
		b.setErr(err.Error())
		return b
	}
	b.clauses = append(b.clauses, expr)
	return b
}

func (b *FilterBuilder) Things(andTerms, orTerms []string) *FilterBuilder {
	if len(andTerms) == 0 && len(orTerms) == 0 {
		b.setErr("things filter requires at least one term")
		return b
	}
	escapedAnd := make([]string, 0, len(andTerms))
	for _, t := range andTerms {
		val, err := escapeFilterValue(strings.TrimSpace(t))
		if err != nil {
			b.setErr("invalid things term: " + err.Error())
			return b
		}
		escapedAnd = append(escapedAnd, val)
	}
	escapedOr := make([]string, 0, len(orTerms))
	for _, t := range orTerms {
		val, err := escapeFilterValue(strings.TrimSpace(t))
		if err != nil {
			b.setErr("invalid things term: " + err.Error())
			return b
		}
		escapedOr = append(escapedOr, val)
	}

	var body string
	switch {
	case len(escapedAnd) > 0 && len(escapedOr) > 0:
		body = strings.Join(escapedAnd, " AND ") + " OR " + strings.Join(escapedOr, " OR ")
	case len(escapedAnd) > 0:
		body = strings.Join(escapedAnd, " AND ")
	default:
		body = strings.Join(escapedOr, " OR ")
	}
	b.clauses = append(b.clauses, "things:("+body+")")
	return b
}

func (b *FilterBuilder) AndRaw(raw string) *FilterBuilder {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		b.setErr("raw filter clause must not be empty")
		return b
	}
	if hasControlChars(trimmed) {
		b.setErr("raw filter clause contains invalid control characters")
		return b
	}
	if len(b.clauses) == 0 {
		b.clauses = append(b.clauses, trimmed)
	} else {
		b.clauses = append(b.clauses, "("+trimmed+")")
	}
	return b
}

func (b *FilterBuilder) Build() (string, error) {
	if b == nil {
		return "", &InvalidFilterError{Message: "filter builder is nil"}
	}
	if b.err != nil {
		return "", b.err
	}
	if len(b.clauses) == 0 {
		return "", &InvalidFilterError{Message: "filter builder has no clauses"}
	}
	return strings.Join(b.clauses, " AND "), nil
}

func formatDisjunction(field string, values []string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("%s filter requires at least one value", field)
	}
	escaped := make([]string, 0, len(values))
	for _, raw := range values {
		val, err := escapeFilterValue(strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("invalid %s value: %w", field, err)
		}
		escaped = append(escaped, val)
	}
	return fmt.Sprintf("%s:(%s)", field, strings.Join(escaped, " OR ")), nil
}

func escapeFilterValue(val string) (string, error) {
	if val == "" {
		return "", fmt.Errorf("value must not be empty")
	}
	if hasControlChars(val) {
		return "", fmt.Errorf("value contains control characters")
	}
	upper := strings.ToUpper(val)
	if upper != "AND" && upper != "OR" && upper != "NOT" && upper != "TO" && isSafeToken(val) {
		return val, nil
	}
	return strconv.Quote(val), nil
}

func isSafeToken(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '#' || r == '/':
		default:
			return false
		}
	}
	return s != ""
}

func hasControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
