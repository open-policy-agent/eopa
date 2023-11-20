package login

import (
	go_open "github.com/skratchdot/open-golang/open"
)

type Opener interface {
	Run(url string) error
}

type defaultOpener struct{}

func (defaultOpener) Run(url string) error {
	return go_open.Run(url)
}
