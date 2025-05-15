package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2/clientcredentials"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func main() {
	// Загружаем переменные из .env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Получаем токены
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN not set")
	}
	clientID := os.Getenv("SPOTIFY_CLIENT_ID")
	clientSecret := os.Getenv("SPOTIFY_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		log.Fatal("SPOTIFY_CLIENT_ID or SPOTIFY_CLIENT_SECRET not set")
	}

	// Настраиваем HTTP-клиент (VPN уже включён на уровне системы)
	httpClient := http.DefaultClient

	// Настраиваем Spotify
	ctx := context.Background()
	config := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     spotifyauth.TokenURL,
	}
	token, err := config.Token(ctx)
	if err != nil {
		log.Fatalf("Could not get Spotify token: %v", err)
	}
	httpClient = spotifyauth.New().Client(ctx, token)
	client := spotify.New(httpClient)

	// Создаём Telegram бота
	bot, err := tgbotapi.NewBotAPI(telegramToken)
	if err != nil {
		log.Fatalf("Cannot create bot: %v", err)
	}

	bot.Debug = true
	log.Printf("Authorized on account %s", bot.Self.UserName)

	// Настраиваем обновления
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Список жанров
	genres := []string{"рок", "поп", "джаз"}

	// Обрабатываем сообщения
	for update := range updates {
		// Обработка команд
		if update.Message != nil {
			if update.Message.Command() == "start" {
				var buttons []tgbotapi.InlineKeyboardButton
				for _, genre := range genres {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(genre, "genre:"+genre))
				}
				buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Случайный жанр", "genre:random"))

				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(buttons...),
				)

				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Выбери жанр:")
				msg.ReplyMarkup = keyboard
				_, err := bot.Send(msg)
				if err != nil {
					log.Printf("Error sending message: %v", err)
				}
			}
		}

		// Обработка нажатий на кнопки
		if update.CallbackQuery != nil {
			callback := update.CallbackQuery
			data := callback.Data

			// Выбор жанра
			if len(data) > 6 && data[:6] == "genre:" {
				genre := data[6:]
				var responseText string
				if genre == "random" {
					responseText = "Ты выбрал случайный жанр! Что хочешь найти?"
				} else {
					responseText = "Ты выбрал жанр: " + genre + ". Что хочешь найти?"
				}

				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Альбом", "choice:album:"+genre),
						tgbotapi.NewInlineKeyboardButtonData("Трек", "choice:track:"+genre),
						tgbotapi.NewInlineKeyboardButtonData("Исполнитель", "choice:artist:"+genre),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("Назад", "back:genres"),
					),
				)

				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, responseText)
				msg.ReplyMarkup = keyboard
				_, err := bot.Send(msg)
				if err != nil {
					log.Printf("Error sending message: %v", err)
				}

				bot.Request(tgbotapi.NewCallback(callback.ID, ""))
			}

			// Обработка выбора типа (альбом, трек, исполнитель)
			if len(data) > 7 && data[:7] == "choice:" {
				parts := splitData(data[7:])
				if len(parts) != 2 {
					continue
				}
				choice, genre := parts[0], parts[1]
				var coverURL, caption string
				var err error

				switch choice {
				case "album":
					coverURL, caption, err = searchAlbum(client, genre)
				case "track":
					coverURL, caption, err = searchTrack(client, genre)
				case "artist":
					coverURL, caption, err = searchArtist(client, genre)
				}

				if err != nil {
					msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Ошибка при поиске: "+err.Error())
					_, err = bot.Send(msg)
					if err != nil {
						log.Printf("Error sending message: %v", err)
					}
				} else {
					// Отправляем фото с обложкой и подписью
					photo := tgbotapi.NewPhoto(callback.Message.Chat.ID, tgbotapi.FileURL(coverURL))
					photo.Caption = caption
					_, err = bot.Send(photo)
					if err != nil {
						log.Printf("Error sending photo: %v", err)
						// Если не удалось отправить фото, отправляем только текст
						msg := tgbotapi.NewMessage(callback.Message.Chat.ID, caption)
						_, err = bot.Send(msg)
						if err != nil {
							log.Printf("Error sending message: %v", err)
						}
					}
				}

				bot.Request(tgbotapi.NewCallback(callback.ID, ""))
			}

			// Обработка кнопки "Назад"
			if data == "back:genres" {
				var buttons []tgbotapi.InlineKeyboardButton
				for _, genre := range genres {
					buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(genre, "genre:"+genre))
				}
				buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData("Случайный жанр", "genre:random"))

				keyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(buttons...),
				)

				msg := tgbotapi.NewMessage(callback.Message.Chat.ID, "Выбери жанр:")
				msg.ReplyMarkup = keyboard
				_, err := bot.Send(msg)
				if err != nil {
					log.Printf("Error sending message: %v", err)
				}

				bot.Request(tgbotapi.NewCallback(callback.ID, ""))
			}
		}
	}
}

// splitData разбивает строку вида "album:жанр" на части
func splitData(data string) []string {
	parts := []string{}
	current := ""
	for _, r := range data {
		if r == ':' {
			if current != "" {
				parts = append(parts, current)
			}
			current = ""
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// mustParseURL парсит URL для прокси
func mustParseURL(proxy string) *url.URL {
	u, err := url.Parse(proxy)
	if err != nil {
		log.Fatalf("Invalid proxy URL: %v", err)
	}
	return u
}

// searchAlbum ищет случайный альбом по жанру
func searchAlbum(client *spotify.Client, genre string) (coverURL, caption string, err error) {
	ctx := context.Background()
	results, err := client.Search(ctx, genre+" album", spotify.SearchTypeAlbum)
	if err != nil {
		return "", "", err
	}
	if results.Albums != nil && len(results.Albums.Albums) > 0 {
		rand.Seed(time.Now().UnixNano())
		album := results.Albums.Albums[rand.Intn(len(results.Albums.Albums))]
		coverURL = album.Images[0].URL // Первая обложка (обычно самая большая)
		caption = album.Artists[0].Name + " — " + album.Name + "\n🟢 Spotify: https://open.spotify.com/album/" + album.ID.String()
		return coverURL, caption, nil
	}
	return "", "Альбомы не найдены", nil
}

// searchTrack ищет случайный трек по жанру
func searchTrack(client *spotify.Client, genre string) (coverURL, caption string, err error) {
	ctx := context.Background()
	results, err := client.Search(ctx, genre+" track", spotify.SearchTypeTrack)
	if err != nil {
		return "", "", err
	}
	if results.Tracks != nil && len(results.Tracks.Tracks) > 0 {
		rand.Seed(time.Now().UnixNano())
		track := results.Tracks.Tracks[rand.Intn(len(results.Tracks.Tracks))]
		coverURL = track.Album.Images[0].URL // Обложка из альбома трека
		caption = track.Artists[0].Name + " — " + track.Name + "\n🟢 Spotify: https://open.spotify.com/track/" + track.ID.String()
		return coverURL, caption, nil
	}
	return "", "Треки не найдены", nil
}

// searchArtist ищет случайного исполнителя по жанру
func searchArtist(client *spotify.Client, genre string) (coverURL, caption string, err error) {
	ctx := context.Background()
	results, err := client.Search(ctx, genre+" artist", spotify.SearchTypeArtist)
	if err != nil {
		return "", "", err
	}
	if results.Artists != nil && len(results.Artists.Artists) > 0 {
		rand.Seed(time.Now().UnixNano())
		artist := results.Artists.Artists[rand.Intn(len(results.Artists.Artists))]
		coverURL = ""
		if len(artist.Images) > 0 {
			coverURL = artist.Images[0].URL // Фото исполнителя
		}
		caption = artist.Name + "\n🟢 Spotify: https://open.spotify.com/artist/" + artist.ID.String()
		return coverURL, caption, nil
	}
	return "", "Исполнители не найдены", nil
}
