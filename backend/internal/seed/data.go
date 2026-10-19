package seed

// Name lists deliberately include Turkish characters (ç, ğ, ı, İ, ö, ş, ü),
// two-word first names and long surnames: they exercise rune-based length
// checks and the worst-case personalization length (DECISIONS.md D15).

var turkishFemaleNames = []string{
	"Ayşe", "Fatma", "Zeynep", "Elif", "Emine", "Hatice", "Merve", "Büşra", "Esra", "Özlem",
	"Gül", "Şeyma", "Damla", "İrem", "Ebru", "Derya", "Selin", "Gizem", "Tuğba", "Sevgi",
	"Çiğdem", "Yağmur", "Dilek", "Pınar", "Nur", "Ece", "Melike", "Sıla", "Ilgın", "Öykü",
	"Fatma Nur", "Ayşe Gül", "Hülya", "Nazlı", "Defne",
}

var turkishMaleNames = []string{
	"Mehmet", "Mustafa", "Ahmet", "Ali", "Hüseyin", "Hasan", "İbrahim", "İsmail", "Osman", "Yusuf",
	"Murat", "Ömer", "Emre", "Burak", "Serkan", "Oğuz", "Çağrı", "Barış", "Tolga", "Uğur",
	"Kerem", "Eren", "Can", "Arda", "Doğukan", "Batuhan", "Yiğit", "Gökhan", "Şahin", "Onur",
	"Abdurrahman", "Muhammed Emin", "Kaan", "Berk", "Selim",
}

var turkishLastNames = []string{
	"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
	"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek",
	"Polat", "Korkmaz", "Erdoğan", "Güneş", "Aktaş", "Ateş", "Bulut", "Keskin", "Ünal", "Güler",
	"Karagöz", "Tekin", "Bozkurt", "Avcı", "Uçar", "Işık", "Gündoğdu", "Karadeniz",
	"Hacıosmanoğlu", "Kocabıyıkoğlu", "Karamanlıoğlu",
}

// English-speaking users (language = "en") get English names.
var englishFirstNames = []string{
	"James", "Emma", "Oliver", "Sophia", "Liam", "Mia", "Noah", "Ava", "Lucas", "Isla",
	"Daniel", "Grace", "Samuel", "Chloe", "Thomas", "Hannah",
}

var englishLastNames = []string{
	"Smith", "Johnson", "Brown", "Taylor", "Wilson", "Davies", "Evans", "Thomas", "Roberts",
	"Walker", "Wright", "Robinson", "Thompson", "Clarke", "Montgomery-Fitzgerald",
}

// Cities and their share of users (weights sum to 100). Spelled canonically,
// with Turkish characters, because segment rules match on these exact values.
var cityWeights = []struct {
	City   string
	Weight int
}{
	{"İstanbul", 30},
	{"Ankara", 12},
	{"İzmir", 10},
	{"Bursa", 6},
	{"Antalya", 6},
	{"Adana", 5},
	{"Konya", 5},
	{"Gaziantep", 4},
	{"Kocaeli", 4},
	{"Eskişehir", 4},
	{"Mersin", 3},
	{"Kayseri", 3},
	{"Samsun", 3},
	{"Trabzon", 3},
	{"Diyarbakır", 2},
}

// Cities returns the canonical city names in weight order.
func Cities() []string {
	out := make([]string, len(cityWeights))
	for i, c := range cityWeights {
		out[i] = c.City
	}
	return out
}
