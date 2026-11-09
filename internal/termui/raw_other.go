//go:build !windows

package termui

func makeRaw() (func(), bool) {
	return func() {}, false
}
