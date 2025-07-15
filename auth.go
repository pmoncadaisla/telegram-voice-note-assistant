package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-faster/errors"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

// termAuth implements the authentication interface for the terminal.
type termAuth struct {
	phone string
}

func (a termAuth) Phone(_ context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	fmt.Print("Enter your phone number: ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) Password(_ context.Context) (string, error) {
	fmt.Print("Enter your 2FA password: ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Enter the verification code: ")
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), nil
}

func (a termAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("sign up not implemented")
}

func (a termAuth) AcceptTermsOfService(ctx context.Context, tos tg.HelpTermsOfService) error {
	fmt.Println("You must accept the terms of service to continue.")
	fmt.Println(tos.Text)
	return errors.New("terms of service not accepted")
}
