package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/caarlos0/env/v10"
	"github.com/chzyer/readline"
	"github.com/joho/godotenv"
	"github.com/mdp/qrterminal/v3"
)

var (
	configDir, _ = os.UserConfigDir()
	envFileName  = filepath.Join(configDir, "notify-tool", "notify.env")

	config = struct {
		Subscriber      string `env:"SUBSCRIBER,required"`
		VAPIDPublicKey  string `env:"PUBLIC_KEY,required"`
		VAPIDPrivateKey string `env:"PRIVATE_KEY,required"`
	}{}
	initFlags  = flag.NewFlagSet("init", flag.ExitOnError)
	subscriber string
	subsFlags  = flag.NewFlagSet("subscribe", flag.ExitOnError)
	listFlags  = flag.NewFlagSet("list", flag.ExitOnError)
	pushFlags  = flag.NewFlagSet("push", flag.ExitOnError)
	title      string
	data       string
)

type Notify struct {
	Title string          `json:"title,omitempty"`
	Body  string          `json:"body,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func Init(ctx context.Context) {
	switch initFlags.NArg() {
	case 0:
	case 1:
		envFileName = initFlags.Arg(0)
	default:
		log.Println("too many arguments")
		initFlags.Usage()
		os.Exit(1)
	}
	vapidPrivateKey, vapidPublicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Fatal(err)
	}
	dir := filepath.Dir(envFileName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	fp, err := os.Create(envFileName)
	if err != nil {
		log.Fatal(err)
	}
	defer fp.Close()
	if _, err := fmt.Fprintf(fp, "SUBSCRIBER=%s\n", subscriber); err != nil {
		log.Fatal(err)
	}
	if _, err := fmt.Fprintf(fp, "PUBLIC_KEY=%s\n", vapidPublicKey); err != nil {
		log.Fatal(err)
	}
	if _, err := fmt.Fprintf(fp, "PRIVATE_KEY=%s\n", vapidPrivateKey); err != nil {
		log.Fatal(err)
	}
	if err := fp.Sync(); err != nil {
		log.Fatal(err)
	}
}

func Subscribe(ctx context.Context) {
	log.Println("load credential:", envFileName)
	if err := godotenv.Load(envFileName); err != nil {
		log.Print(err)
	}
	if err := env.Parse(&config); err != nil {
		log.Fatal(err)
	}
	url := fmt.Sprintf("https://pages.switch-science.com/notify-app/?pubKey=%s", config.VAPIDPublicKey)
	qrterminal.GenerateWithConfig(url, qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 2,
		WithSixel: false, //qrterminal.IsSixelSupported(os.Stdout),
	})
	log.Println("open:", url)
	rl, err := readline.New("subscription:> ")
	if err != nil {
		log.Fatal(err)
	}
	defer rl.Close()
	line, err := rl.Readline()
	if err != nil { // io.EOF
		log.Fatal(err)
	}
	log.Println(line)
	var subs *webpush.Subscription
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&subs); err != nil {
		log.Fatal(err)
	}
	dir := filepath.Dir(envFileName)
	os.MkdirAll(filepath.Join(dir, "subscriptions"), 0o755)
	fp, err := os.Create(filepath.Join(configDir, "subscriptions", subs.Keys.P256dh+".json"))
	if err != nil {
		log.Fatal(err)
	}
	defer fp.Close()
	encoder := json.NewEncoder(fp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(subs); err != nil {
		log.Fatal(err)
	}
}

func List(ctx context.Context) {
	dir := os.DirFS(filepath.Join(filepath.Dir(envFileName), "subscriptions"))
	if err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		log.Println(path)
		return nil
	}); err != nil {
		log.Fatal(err)
	}
}

func Push(ctx context.Context) {
	log.Println("load credential:", envFileName)
	if err := godotenv.Load(envFileName); err != nil {
		log.Print(err)
	}
	if err := env.Parse(&config); err != nil {
		log.Fatal(err)
	}
	dir := os.DirFS(filepath.Join(filepath.Dir(envFileName), "subscriptions"))
	if err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".json") {
			return nil
		}
		fpath := filepath.Join("subscriptions", path)
		log.Println("load subscription:", fpath)
		fp, err := os.Open(fpath)
		if err != nil {
			return err
		}
		defer fp.Close()
		var s *webpush.Subscription
		if err := json.NewDecoder(fp).Decode(&s); err != nil {
			return err
		}
		notify := &Notify{
			Title: title,
			Body:  strings.Join(pushFlags.Args(), " "),
		}
		if data != "" {
			notify.Data = []byte(data)
		}
		b, err := json.Marshal(notify)
		if err != nil {
			return err
		}
		resp, err := webpush.SendNotificationWithContext(ctx, b, s, &webpush.Options{
			Subscriber:      config.Subscriber,
			VAPIDPublicKey:  config.VAPIDPublicKey,
			VAPIDPrivateKey: config.VAPIDPrivateKey,
			TTL:             30,
		})
		if err != nil {
			return err
		}
		log.Println(resp)
		return nil
	}); err != nil {
		log.Println(err)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.SetFlags(log.Lshortfile)
	flag.Parse()
	switch flag.Arg(0) {
	case initFlags.Name():
		initFlags.StringVar(&subscriber, "subscriber", "example@example.org", "subscriber")
		if err := initFlags.Parse(flag.Args()[1:]); err != nil {
			log.Println(err)
			initFlags.Usage()
			os.Exit(1)
		}
		Init(ctx)
		os.Exit(0)
	case subsFlags.Name():
		subsFlags.StringVar(&envFileName, "env", envFileName, "load .env file")
		if err := subsFlags.Parse(flag.Args()[1:]); err != nil {
			log.Println(err)
			subsFlags.Usage()
			os.Exit(1)
		}
		Subscribe(ctx)
		os.Exit(0)
	case listFlags.Name():
		List(ctx)
		os.Exit(0)
	case pushFlags.Name():
		pushFlags.StringVar(&title, "title", "", "title")
		pushFlags.StringVar(&data, "data", "", "data")
		pushFlags.StringVar(&envFileName, "env", envFileName, "load .env file")
		if err := pushFlags.Parse(flag.Args()[1:]); err != nil {
			log.Println(err)
			pushFlags.Usage()
			os.Exit(1)
		}
		Push(ctx)
		os.Exit(0)
	case "":
		flag.Usage()
		os.Exit(1)
	default:
		log.Println("unknown sub-command:", flag.Arg(0))
		flag.Usage()
		os.Exit(1)
	}
}
