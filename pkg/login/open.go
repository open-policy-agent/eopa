package login

import (
	go_open "github.com/skratchdot/open-golang/open"
)

func Reset() {
	open = defaultOpener{}
}

func SetOpener(o Opener) {
	open = o
}

type Opener interface {
	Run(url string) error
}

var open Opener = defaultOpener{}

type defaultOpener struct{}

func (defaultOpener) Run(url string) error {
	return go_open.Run(url)
}
