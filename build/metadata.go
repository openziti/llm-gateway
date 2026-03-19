package build

import "fmt"

var Version string
var Hash string
var Date string
var BuiltBy string

const Series = "v0.1"

func String() string {
	if Version != "" {
		return fmt.Sprintf("%v [%v]", Version, Hash)
	} else {
		return Series + ".x [developer build]"
	}
}
