// Package idutil translates between provider-native thread/message id formats
// (Gmail, Outlook/Graph) and the keys we store in the DB. Pure functions, no I/O.
package idutil

import (
	"encoding/base64"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// GmailResult is the structured classification of a Gmail id token.
// Hex is empty and Decimal is 0 when the id is not base-convertible
// (client-local drafts, opaque web blobs, unknown shapes).
type GmailResult struct {
	Input   string `json:"input"`
	Kind    string `json:"kind"` // legacy-f|web|local|web-opaque|decimal|hex|unknown
	Hex     string `json:"hex"`
	Decimal uint64 `json:"decimal"`
	ThreadF string `json:"thread_f"`
	MsgF    string `json:"msg_f"`
	Note    string `json:"note"`
	WebURL  string `json:"web_url"`
}

var (
	reGmailWebDecoded = regexp.MustCompile(`(?:^|-)f:(\d{1,20})$`)
	reGmailLegacyF    = regexp.MustCompile(`^(?:thread|msg)-f:(\d+)$`)
	reGmailLocalA     = regexp.MustCompile(`^(?:thread|msg)-a:.+$`)
	reGmailDraftR     = regexp.MustCompile(`^r\d{6,}$`)
	reGmailB64url     = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)
	reGmailHex16      = regexp.MustCompile(`^[0-9a-fA-F]{16}$`)
	reGmailDecimal    = regexp.MustCompile(`^\d{15,20}$`)
)

// ClassifyGmail identifies a Gmail thread/message id token and, where the id is
// the IMAP-derived 64-bit value, derives its hex/decimal/legacy-f forms + web URL.
func ClassifyGmail(token string) GmailResult {
	return classifyGmailUser(token, "0")
}

// ClassifyGmailForUser is like ClassifyGmail but builds the web URL for the
// given Gmail /u/N/ mailbox index.
func ClassifyGmailForUser(token, user string) GmailResult {
	return classifyGmailUser(token, user)
}

func classifyGmailUser(token, user string) GmailResult {
	res := GmailResult{Input: token}
	t := strings.TrimSpace(token)

	if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
		var hexOnly bool
		t, hexOnly = gmailURLToken(t)
		// th=/#folder/ ids are hex even when every digit happens to be 0-9.
		if hexOnly && reGmailHex16.MatchString(t) {
			dec, _ := strconv.ParseUint(strings.ToLower(t), 16, 64)
			res.Kind = "hex"
			fillGmailHex(&res, dec, user)
			return res
		}
	}

	// Legacy prefixed forms: thread-f:<dec> / msg-f:<dec> — decimal of the hex id.
	if m := reGmailLegacyF.FindStringSubmatch(t); m != nil {
		dec, _ := strconv.ParseUint(m[1], 10, 64)
		res.Kind = "legacy-f"
		fillGmailHex(&res, dec, user)
		return res
	}

	// Client-local (drafts / all-mail) — not hex-derived.
	if reGmailLocalA.MatchString(t) || reGmailDraftR.MatchString(t) {
		res.Kind = "local"
		res.Note = "client-local / draft id — no hex equivalent"
		return res
	}

	// Web-UI token (#inbox/FMfcg…): the vowel-free alphabet re-encodes "thread-f:<dec>" losslessly.
	if dec, ok := decodeGmailWebToken(t); ok {
		res.Kind = "web"
		fillGmailHex(&res, dec, user)
		return res
	}

	// Opaque web blob: long base64url, mixed case, NOT a clean 16-hex id.
	if reGmailB64url.MatchString(t) && !reGmailHex16.MatchString(t) {
		raw := decodeURLSafe(t)
		res.Kind = "web-opaque"
		res.Note = "opaque " + strconv.Itoa(len(raw)) + "-byte server token — not offline-convertible"
		return res
	}

	// Bare decimal (the 64-bit int).
	if reGmailDecimal.MatchString(t) {
		dec, _ := strconv.ParseUint(t, 10, 64)
		res.Kind = "decimal"
		fillGmailHex(&res, dec, user)
		return res
	}

	// Hex id.
	if reGmailHex16.MatchString(t) {
		dec, _ := strconv.ParseUint(strings.ToLower(t), 16, 64)
		res.Kind = "hex"
		fillGmailHex(&res, dec, user)
		return res
	}

	res.Kind = "unknown"
	res.Note = "unrecognized id format"
	return res
}

// fillGmailHex populates the hex/decimal/legacy + URL fields from the 64-bit id.
func fillGmailHex(res *GmailResult, dec uint64, user string) {
	hex := strconv.FormatUint(dec, 16)
	res.Hex = hex
	res.Decimal = dec
	res.ThreadF = "thread-f:" + strconv.FormatUint(dec, 10)
	res.MsgF = "msg-f:" + strconv.FormatUint(dec, 10)
	res.WebURL = GmailWebURL(hex, user)
}

// GmailWebURL builds a clickable thread URL. Gmail resolves a legacy hex id
// dropped into the URL hash, so #all/<hex> opens the conversation.
func GmailWebURL(hexid, user string) string {
	return "https://mail.google.com/mail/u/" + user + "/#all/" + hexid
}

// decodeURLSafe decodes a base64url token, padding as needed; b” on failure.
func decodeURLSafe(s string) []byte {
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return raw
}

// gmailURLToken pulls the id out of a pasted Gmail URL: the print/permalink query
// (permthid=thread-f:<dec>, th=<hex>) first, else the last '#folder/…/<id>' segment. hexOnly marks
// the positions that only ever carry a hex id.
func gmailURLToken(raw string) (token string, hexOnly bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return raw, false
	}
	q := u.Query()
	if v := q.Get("permthid"); v != "" {
		return v, false
	}
	if v := q.Get("th"); v != "" {
		return v, true
	}
	frag, _, _ := strings.Cut(u.Fragment, "?")
	if i := strings.LastIndex(frag, "/"); i >= 0 {
		return frag[i+1:], true
	}
	return raw, false
}

// gmailWebAlphabet is the consonant alphabet of Gmail's web-UI ids; read as a base-40 number and
// re-spelled in base64 it decodes to the tail of "thread-f:<dec>" (client-local ids yield "a:…").
const gmailWebAlphabet = "BCDFGHJKLMNPQRSTVWXZbcdfghjklmnpqrstvwxz"

func decodeGmailWebToken(t string) (uint64, bool) {
	if len(t) < 20 || strings.Trim(t, gmailWebAlphabet) != "" {
		return 0, false
	}
	n, base := new(big.Int), big.NewInt(int64(len(gmailWebAlphabet)))
	for _, r := range t {
		n.Mul(n, base).Add(n, big.NewInt(int64(strings.IndexRune(gmailWebAlphabet, r))))
	}
	const b64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var digits []byte
	sixtyFour, mod := big.NewInt(64), new(big.Int)
	for n.Sign() > 0 {
		n.DivMod(n, sixtyFour, mod)
		digits = append([]byte{b64[mod.Int64()]}, digits...)
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(string(digits), "="))
	if err != nil {
		return 0, false
	}
	m := reGmailWebDecoded.FindStringSubmatch(string(raw))
	if m == nil {
		return 0, false
	}
	dec, err := strconv.ParseUint(m[1], 10, 64)
	return dec, err == nil
}
