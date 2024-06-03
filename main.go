package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
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
	//decoder.DisallowUnknownFields()
	if err := decoder.Decode(&subs); err != nil {
		log.Fatal(err)
	}
	dir := filepath.Dir(envFileName)
	os.MkdirAll(filepath.Join(dir, "subscriptions"), 0o755)
	fp, err := os.Create(filepath.Join(dir, "subscriptions", subs.Keys.P256dh+".json"))
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
	dir := filepath.Join(filepath.Dir(envFileName), "subscriptions")
	subs := os.DirFS(dir)
	files, err := fs.Glob(subs, "*.json")
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		log.Println(filepath.Join(dir, f))
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
	dir := filepath.Join(filepath.Dir(envFileName), "subscriptions")
	subs := os.DirFS(dir)
	files, err := fs.Glob(subs, "*.json")
	if err != nil {
		log.Fatal(err)
	}
	for _, path := range files {
		fpath := filepath.Join(dir, path)
		log.Println("load subscription:", fpath)
		subs, err := os.ReadFile(fpath)
		if err != nil {
			log.Print(err)
			continue
		}
		var s *webpush.Subscription
		if err := json.Unmarshal(subs, &s); err != nil {
			log.Print(err)
			continue
		}
		notify := &Notify{
			Title: title,
			Body:  strings.Join(pushFlags.Args(), " "),
		}
		if data != "" {
			notify.Data = []byte(data)
		}
		bin, err := json.Marshal(notify)
		if err != nil {
			log.Print(err)
			continue
		}
		resp, err := webpush.SendNotificationWithContext(ctx, bin, s, &webpush.Options{
			Subscriber:      config.Subscriber,
			VAPIDPublicKey:  config.VAPIDPublicKey,
			VAPIDPrivateKey: config.VAPIDPrivateKey,
			TTL:             30,
		})
		if err != nil {
			log.Print(err)
			continue
		}
		log.Println(resp.StatusCode, resp.Status)
		if resp.StatusCode != http.StatusCreated {
			d := filepath.Dir(fpath)
			os.Mkdir(filepath.Join(d, "revoked"), 0o755)
			n := filepath.Base(fpath)
			dst := filepath.Join(d, "revoked", n)
			log.Println(fpath, "->", dst)
			if err := os.Rename(fpath, dst); err != nil {
				log.Print(err)
			}
			log.Print(fmt.Errorf("status code: %d", resp.StatusCode))
			continue
		}
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
		listFlags.StringVar(&envFileName, "env", envFileName, "load .env file")
		if err := listFlags.Parse(flag.Args()[1:]); err != nil {
			log.Println(err)
			listFlags.Usage()
			os.Exit(1)
		}
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
