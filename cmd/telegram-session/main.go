//go:build telegram

// telegram-session создаёт локальный MTProto session-файл для закрытого volume.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// main выполняет интерактивную авторизацию, не печатая session или credentials.
func main() {
	apiID, err := strconv.Atoi(os.Getenv("TELEGRAM_API_ID"))
	if err != nil || apiID == 0 {
		fail("TELEGRAM_API_ID должен быть числом")
	}

	apiHash := os.Getenv("TELEGRAM_API_HASH")
	phone := os.Getenv("TELEGRAM_PHONE")
	sessionPath := os.Getenv("TELEGRAM_SESSION_PATH")
	if apiHash == "" || phone == "" || sessionPath == "" {
		fail("TELEGRAM_API_HASH, TELEGRAM_PHONE и TELEGRAM_SESSION_PATH обязательны")
	}

	client := telegram.NewClient(apiID, apiHash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: sessionPath},
	})

	reader := bufio.NewReader(os.Stdin)

	code := auth.CodeAuthenticatorFunc(func(_ context.Context, _ *tg.AuthSentCode) (string, error) {
		fmt.Print("Код из Telegram: ")
		value, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read Telegram code: %w", err)
		}

		return strings.TrimSpace(value), nil
	})

	authenticator := auth.CodeOnly(phone, code)
	if password := os.Getenv("TELEGRAM_2FA_PASSWORD"); password != "" {
		authenticator = auth.Constant(phone, password, code)
	}

	err = client.Run(context.Background(), func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("read authorization status: %w", err)
		}
		if status.Authorized {
			return nil
		}

		return auth.NewFlow(authenticator, auth.SendCodeOptions{}).Run(ctx, client.Auth())
	})
	if err != nil {
		fail(err.Error())
	}

	fmt.Printf("Session сохранена: %s\n", sessionPath)
}

// fail завершает утилиту без stack trace и вывода секретов.
func fail(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
