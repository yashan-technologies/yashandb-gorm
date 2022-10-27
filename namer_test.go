package yasdb

import "testing"

func TestTryRemoveQuotes(t *testing.T) {
    name := []string{`"USER"`, `USER`, `"U"`, `"U`, `U`}
    wantName := []string{`USER`, `USER`, `U`, `"U`, `U`}

    for i, v := range name {
        getName := TryRemoveQuotes(v)
        if getName != wantName[i] {
            t.Fatalf("TryRemoveQuotes function err: name - %s; getName - %s; wantName - %s", v, getName, wantName[i])
        }
    }
}
