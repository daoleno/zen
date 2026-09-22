package work

import "fmt"

func lockDSHSession(string) (func(), error) {
	return nil, fmt.Errorf("DSH Session bridge requires a Unix daemon host")
}
