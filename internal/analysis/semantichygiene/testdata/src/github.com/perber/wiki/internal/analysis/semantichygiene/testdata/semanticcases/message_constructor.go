package semanticcases

import "strings"

func MessageIDForTrimmedCode(code ErrorCode) MessageID {
	trimmed := strings.TrimSpace(string(code))
	if trimmed == "" {
		return ""
	}
	return MessageID("errors." + trimmed)
}

func MessageIDForCodeViaLocalTrim(code ErrorCode) MessageID {
	trimmed := TrimSpace(string(code)) // want "semantic value ErrorCode converted to string before internal call TrimSpace; make the callee accept ErrorCode"
	return MessageID("errors." + trimmed)
}

func MessageIDForCodeViaNestedLocalTrim(code ErrorCode) MessageID {
	return MessageID("errors." + TrimSpace(string(code))) // want "semantic value ErrorCode converted to string before internal call TrimSpace; make the callee accept ErrorCode"
}

func PageIDForUserID(userID UserID) PageID {
	return PageID(userID.String()) // want "semantic value UserID converted to string before internal call PageID; make the callee accept UserID"
}
