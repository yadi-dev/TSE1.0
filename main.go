package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Conversation states
const (
	StateWaitingForPhoto = iota
	StateWaitingForCoordinates
)

// User session data
type UserSession struct {
	State    int
	FileData []byte
	FileName string
}

var userSessions = map[int64]*UserSession{}

func decimalToDMS(deg float64) (int, int, float64) {
	d := int(math.Abs(deg))
	mf := (math.Abs(deg) - float64(d)) * 60
	m := int(mf)
	s := (mf - float64(m)) * 60
	return d, m, s
}

func main() {
	bot, err := tgbot.NewBotAPI("7983354002:AAFcf48DAJG8EswGaLgAszVmqtvdkVMKGcI")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Bot berjalan...")

	u := tgbot.NewUpdate(0)
	u.Timeout = 30
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID

		// ======================
		// COMMAND HANDLING
		// ======================
		if update.Message.IsCommand() {
			switch update.Message.Command() {
			case "start":
				handleStart(bot, chatID)
			case "batal", "cancel":
				handleCancel(bot, chatID)
			default:
				handleUnknown(bot, chatID)
			}
			continue
		}

		// ======================
		// AUTO-HANDLE PHOTO WITHOUT SESSION
		// ======================
		if update.Message.Document != nil &&
			(strings.HasSuffix(strings.ToLower(update.Message.Document.FileName), ".jpg") ||
				strings.HasSuffix(strings.ToLower(update.Message.Document.FileName), ".jpeg")) {

			// Auto-create session if doesn't exist
			_, exists := userSessions[chatID]
			if !exists {
				userSessions[chatID] = &UserSession{
					State: StateWaitingForPhoto,
				}
			}

			// Handle photo regardless of current state
			handlePhotoReceived(bot, chatID, update)
			continue
		}

		// ======================
		// STATE-BASED MESSAGE HANDLING
		// ======================
		session, exists := userSessions[chatID]
		if !exists {
			// Auto-initialize session for new users
			userSessions[chatID] = &UserSession{
				State: StateWaitingForPhoto,
			}
			handleStart(bot, chatID)
			continue
		}

		switch session.State {
		case StateWaitingForPhoto:
			// Check if it's a valid photo document
			if update.Message.Document != nil &&
				(strings.HasSuffix(strings.ToLower(update.Message.Document.FileName), ".jpg") ||
					strings.HasSuffix(strings.ToLower(update.Message.Document.FileName), ".jpeg")) {
				handlePhotoReceived(bot, chatID, update)
			} else {
				msg := tgbot.NewMessage(chatID, "Harap kirimkan **foto** sebagai **dokumen/file** dalam format JPG/JPEG.")
				msg.ParseMode = "Markdown"
				bot.Send(msg)
			}
		case StateWaitingForCoordinates:
			handleCoordinatesReceived(bot, chatID, update)
		default:
			handleUnknown(bot, chatID)
		}
	}
}

// Handler functions
func handleStart(bot *tgbot.BotAPI, chatID int64) {
	// Initialize user session
	userSessions[chatID] = &UserSession{
		State: StateWaitingForPhoto,
	}

	msg := tgbot.NewMessage(chatID,
		"*Halo, Selamat datang di TSE Chat 1.0 Bot penambah Metadata*\n\n"+
			"Kirimkan foto sebagai **dokumen/file** (JPG/JPEG) untuk menambahkan metadata GPS.\n\n")
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func handleCancel(bot *tgbot.BotAPI, chatID int64) {
	session, exists := userSessions[chatID]
	if exists {
		session.State = StateWaitingForPhoto
		session.FileData = nil
		session.FileName = ""
	}
	msg := tgbot.NewMessage(chatID, "Operasi dibatalkan. Kirimkan foto lain untuk memulai.")
	bot.Send(msg)
}

func handleUnknown(bot *tgbot.BotAPI, chatID int64) {
	msg := tgbot.NewMessage(chatID,
		"Tidak dapat memproses pesan ini !\n\n"+
			"Ketik /start untuk memulai.\n\n"+
			"Kirimkan *foto sebagai dokumen* (JPG/JPEG) untuk menambahkan metadata GPS.\n"+
			"Atau kirimkan **koordinat** jika sudah ada foto yang diproses.")
	bot.Send(msg)
}

func handlePhotoReceived(bot *tgbot.BotAPI, chatID int64, update tgbot.Update) {
	if update.Message.Document == nil {
		msg := tgbot.NewMessage(chatID, "Harap kirimkan *foto* sebagai *dokumen/file*.")
		msg.ParseMode = "Markdown"
		bot.Send(msg)
		return
	}

	doc := update.Message.Document
	if !strings.HasSuffix(strings.ToLower(doc.FileName), ".jpg") &&
		!strings.HasSuffix(strings.ToLower(doc.FileName), ".jpeg") {
		msg := tgbot.NewMessage(chatID, "Harap kirimkan file gambar dalam format JPG/JPEG.")
		bot.Send(msg)
		return
	}

	file, err := bot.GetFile(tgbot.FileConfig{FileID: doc.FileID})
	if err != nil {
		msg := tgbot.NewMessage(chatID, "Gagal mendapatkan file.")
		bot.Send(msg)
		return
	}

	url := file.Link(bot.Token)
	data, err := download(url)
	if err != nil {
		msg := tgbot.NewMessage(chatID, "Gagal download foto.")
		bot.Send(msg)
		return
	}

	// Update session
	session := userSessions[chatID]
	session.FileData = data
	session.FileName = doc.FileName
	session.State = StateWaitingForCoordinates

	msg := tgbot.NewMessage(chatID,
		"*Foto diterima !*\n\n"+
			"Sekarang kirimkan **lokasi** (gunakan fitur 'Location' Telegram)\n"+
			"Atau ketik **koordinat GPS** (contoh: `-6.2088,106.8456`)")
	msg.ParseMode = "Markdown"
	bot.Send(msg)
}

func handleCoordinatesReceived(bot *tgbot.BotAPI, chatID int64, update tgbot.Update) {
	session := userSessions[chatID]
	var lat, lon float64

	if update.Message.Location != nil {
		lat = update.Message.Location.Latitude
		lon = update.Message.Location.Longitude
	} else if update.Message.Text != "" {
		parts := strings.Split(update.Message.Text, ",")
		if len(parts) != 2 {
			msg := tgbot.NewMessage(chatID, "Format koordinat tidak valid. Harap kirimkan dalam format 'lat, lon'.")
			bot.Send(msg)
			return
		}

		var err error
		lat, err = strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			msg := tgbot.NewMessage(chatID, "Koordinat harus berupa angka. Coba lagi.")
			bot.Send(msg)
			return
		}

		lon, err = strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil {
			msg := tgbot.NewMessage(chatID, "Koordinat harus berupa angka. Coba lagi.")
			bot.Send(msg)
			return
		}
	} else {
		msg := tgbot.NewMessage(chatID, "Harap kirimkan Lokasi Telegram atau koordinat dalam format teks.")
		bot.Send(msg)
		return
	}

	// Process EXIF with datetime and GPS
	jakartaTime := time.Now().In(time.FixedZone("WIB", 7*3600)) // Asia/Jakarta
	exified, err := addExifMetadata(session.FileData, lat, lon, jakartaTime)
	if err != nil {
		log.Printf("Error processing EXIF: %v", err)
		msg := tgbot.NewMessage(chatID, fmt.Sprintf("Terjadi kesalahan saat memproses foto. Pastikan itu adalah file JPG yang valid. Error: %v", err))
		bot.Send(msg)
		return
	}

	// Send processed photo
	fileName := fmt.Sprintf("TSE1_%s.jpg", jakartaTime.Format("20060102_150405"))
	file := tgbot.FileBytes{
		Name:  fileName,
		Bytes: exified,
	}

	msg := tgbot.NewDocument(chatID, file)
	msg.Caption = fmt.Sprintf("*Metadata berhasil ditambahkan !*\n\n"+
		"Lat: %.4f, Lon: %.4f\n"+
		"Waktu: %s\n\n"+
		"*Kirimkan foto lain sebagai dokumen untuk menambahkan metadata lagi*",
		lat, lon, jakartaTime.Format("2006:01:02 15:04:05"))
	msg.ParseMode = "Markdown"
	bot.Send(msg)

	// Reset session to wait for next photo instead of deleting
	session.State = StateWaitingForPhoto
	session.FileData = nil
	session.FileName = ""
}

func download(url string) ([]byte, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

func addExifMetadata(data []byte, lat, lon float64, timestamp time.Time) ([]byte, error) {
	log.Printf("Adding EXIF metadata: lat=%.6f, lon=%.6f", lat, lon)

	// Create proper EXIF data
	exifData, err := createProperExifData(lat, lon, timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to create EXIF data: %v", err)
	}

	// Embed EXIF back into JPEG
	result, err := embedExifInJpeg(data, exifData)
	if err != nil {
		log.Printf("Failed to embed EXIF: %v", err)
		return nil, fmt.Errorf("failed to embed EXIF: %v", err)
	}

	log.Printf("Successfully added EXIF metadata, result size: %d bytes", len(result))
	return result, nil
}

func createProperExifData(lat, lon float64, timestamp time.Time) ([]byte, error) {
	var buf bytes.Buffer

	// TIFF header (little-endian)
	buf.Write([]byte{0x49, 0x49})             // "II" - little endian
	buf.Write([]byte{0x2A, 0x00})             // TIFF magic number
	buf.Write([]byte{0x08, 0x00, 0x00, 0x00}) // Offset to first IFD

	// Calculate positions
	ifd0Start := 8
	dateTimeStrWithNull := timestamp.Format("2006:01:02 15:04:05") + "\x00"

	// IFD0 entries
	numEntries := uint16(4) // DateTime, DateTimeOriginal, DateTimeDigitized, GPS IFD pointer
	writeUint16(&buf, numEntries)

	// Calculate data offsets
	ifd0DataStart := ifd0Start + 2 + int(numEntries)*12 + 4 // 2 bytes for count + entries + 4 bytes for next IFD
	dateTimeOffset := ifd0DataStart
	dateTimeOriginalOffset := dateTimeOffset + len(dateTimeStrWithNull)
	dateTimeDigitizedOffset := dateTimeOriginalOffset + len(dateTimeStrWithNull)
	gpsIfdOffset := dateTimeDigitizedOffset + len(dateTimeStrWithNull)

	// DateTime entry
	writeIfdEntry(&buf, 0x0132, 2, uint32(len(dateTimeStrWithNull)), uint32(dateTimeOffset))

	// DateTimeOriginal entry
	writeIfdEntry(&buf, 0x9003, 2, uint32(len(dateTimeStrWithNull)), uint32(dateTimeOriginalOffset))

	// DateTimeDigitized entry
	writeIfdEntry(&buf, 0x9004, 2, uint32(len(dateTimeStrWithNull)), uint32(dateTimeDigitizedOffset))

	// GPS IFD pointer entry
	writeIfdEntry(&buf, 0x8825, 4, 1, uint32(gpsIfdOffset))

	// Next IFD offset (0)
	writeUint32(&buf, 0)

	// Write DateTime strings
	buf.WriteString(dateTimeStrWithNull) // DateTime
	buf.WriteString(dateTimeStrWithNull) // DateTimeOriginal
	buf.WriteString(dateTimeStrWithNull) // DateTimeDigitized

	// GPS IFD
	gpsNumEntries := uint16(7) // Version, LatRef, Lat, LonRef, Lon, TimeStamp, DateStamp
	writeUint16(&buf, gpsNumEntries)

	// Convert coordinates to DMS
	latD, latM, latS := decimalToDMS(lat)
	lonD, lonM, lonS := decimalToDMS(lon)

	// Calculate GPS data offsets
	gpsDataStart := gpsIfdOffset + 2 + int(gpsNumEntries)*12 + 4 // GPS IFD entries + next IFD
	latDataOffset := gpsDataStart
	lonDataOffset := latDataOffset + 24 // 3 rationals * 8 bytes each
	timeDataOffset := lonDataOffset + 24

	// GPS Version ID
	writeIfdEntry(&buf, 0x0000, 1, 4, 0x00030002) // Version 2.3.0.0 in value field

	// GPS Latitude Reference
	latRef := uint32('N')
	if lat < 0 {
		latRef = uint32('S')
	}
	writeIfdEntry(&buf, 0x0001, 2, 2, latRef) // N/S in value field

	// GPS Latitude
	writeIfdEntry(&buf, 0x0002, 5, 3, uint32(latDataOffset))

	// GPS Longitude Reference
	lonRef := uint32('E')
	if lon < 0 {
		lonRef = uint32('W')
	}
	writeIfdEntry(&buf, 0x0003, 2, 2, lonRef) // E/W in value field

	// GPS Longitude
	writeIfdEntry(&buf, 0x0004, 5, 3, uint32(lonDataOffset))

	// GPS TimeStamp
	writeIfdEntry(&buf, 0x0007, 5, 3, uint32(timeDataOffset))

	// GPS DateStamp
	dateStamp := timestamp.UTC().Format("2006:01:02") + "\x00"
	datePaddedValue := uint32(0)
	if len(dateStamp) <= 4 {
		for i := 0; i < len(dateStamp) && i < 4; i++ {
			datePaddedValue |= uint32(dateStamp[i]) << (8 * i)
		}
	}
	writeIfdEntry(&buf, 0x001D, 2, uint32(len(dateStamp)), datePaddedValue)

	// GPS next IFD (0)
	writeUint32(&buf, 0)

	// GPS Latitude rationals (degrees, minutes, seconds)
	writeRational(&buf, uint32(latD), 1)
	writeRational(&buf, uint32(latM), 1)
	writeRational(&buf, uint32(latS*1000000), 1000000)

	// GPS Longitude rationals
	writeRational(&buf, uint32(lonD), 1)
	writeRational(&buf, uint32(lonM), 1)
	writeRational(&buf, uint32(lonS*1000000), 1000000)

	// GPS TimeStamp rationals
	gpsTime := timestamp.UTC()
	writeRational(&buf, uint32(gpsTime.Hour()), 1)
	writeRational(&buf, uint32(gpsTime.Minute()), 1)
	writeRational(&buf, uint32(gpsTime.Second()), 1)

	return buf.Bytes(), nil
}

func writeUint16(buf *bytes.Buffer, val uint16) {
	buf.WriteByte(byte(val))
	buf.WriteByte(byte(val >> 8))
}

func writeUint32(buf *bytes.Buffer, val uint32) {
	buf.WriteByte(byte(val))
	buf.WriteByte(byte(val >> 8))
	buf.WriteByte(byte(val >> 16))
	buf.WriteByte(byte(val >> 24))
}

func writeIfdEntry(buf *bytes.Buffer, tag uint16, fieldType uint16, count uint32, valueOffset uint32) {
	writeUint16(buf, tag)
	writeUint16(buf, fieldType)
	writeUint32(buf, count)
	writeUint32(buf, valueOffset)
}

func writeRational(buf *bytes.Buffer, numerator, denominator uint32) {
	writeUint32(buf, numerator)
	writeUint32(buf, denominator)
}

func embedExifInJpeg(jpegData, exifData []byte) ([]byte, error) {
	// Check for valid JPEG
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		return nil, fmt.Errorf("not a valid JPEG file")
	}

	// Create new JPEG with EXIF
	result := make([]byte, 0, len(jpegData)+len(exifData)+20)

	// Add SOI marker
	result = append(result, 0xFF, 0xD8)

	// Add APP1 segment for EXIF
	result = append(result, 0xFF, 0xE1) // APP1 marker

	exifSegmentLength := len(exifData) + 6 // +6 for "Exif\x00\x00"
	result = append(result, byte(exifSegmentLength>>8), byte(exifSegmentLength&0xFF))
	result = append(result, []byte("Exif\x00\x00")...) // EXIF identifier
	result = append(result, exifData...)

	// Find the first segment after SOI in original JPEG
	i := 2
	for i < len(jpegData) {
		if jpegData[i] == 0xFF {
			if jpegData[i+1] == 0xE0 || jpegData[i+1] == 0xE1 {
				// Skip existing APP0/APP1 segments
				segmentLength := int(jpegData[i+2])<<8 | int(jpegData[i+3])
				i += 2 + segmentLength
			} else {
				// This is where we start copying
				break
			}
		} else {
			break
		}
	}

	// Add the rest of the JPEG data
	result = append(result, jpegData[i:]...)

	return result, nil
}
