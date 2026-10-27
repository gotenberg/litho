package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The revisions the files come from. Downloads use commits, which cannot
// move. veraPDFCommit is the veraPDF-parser commit tagged v1.30.2, the parser
// veraPDF 1.30.2 bundles (test/integration/corpus/versions.env); its
// font/cmap resources are byte-identical to those the pinned veraPDF CLI jar
// holds. The license comes from Adobe's cmap-resources, the upstream of every
// file.
const (
	veraPDFRepo    = "https://github.com/veraPDF/veraPDF-parser"
	veraPDFCommit  = "40b7f7ca84d340adfe14d4b17860090026a4e28a"
	veraPDFRawURL  = "https://raw.githubusercontent.com/veraPDF/veraPDF-parser"
	veraPDFDir     = "src/main/resources/font/cmap"
	adobeRepo      = "https://github.com/adobe-type-tools/cmap-resources"
	adobeCommit    = "f5cf3bca7fdfeaceb77aa82847e974f2306c20b4"
	adobeRawURL    = "https://raw.githubusercontent.com/adobe-type-tools/cmap-resources"
	licenseSource  = "LICENSE.md"
	licenseName    = "LICENSE.txt"
	outDirName     = "verapdfcmaps"
	outGoName      = "verapdf_cmaps_sources.go"
	generatorLabel = "cmd/gen-verapdf-cmaps"
)

// maxResourceBytes caps one downloaded file. Anti-DoS guard for a build-time
// tool, not a format limit: the largest file, UniGB-UTF32-H, is about 210 KB,
// so 1 MiB leaves room for upstream growth while refusing a runaway response.
const maxResourceBytes = 1 << 20

// httpClient bounds how long the generator waits for GitHub. The bare
// http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	errDigestMismatch = errors.New("SHA-256 differs from the pinned digest")
	errTooLarge       = errors.New("larger than the download cap")
)

// resource names one file of a pinned commit and its SHA-256.
type resource struct {
	Name   string // file name, also the CMap name
	SHA256 string // hex digest of the file as published
}

// resources lists the encoding CMaps veraPDF 1.30.2 bundles whose code-to-CID
// content litho's PDF.js copies do not hold: every one PDF.js ships no copy
// of, and the Unicode CMaps whose cidchar and cidrange mappings differ from
// PDF.js's (GBK2K-H, UniCNS-UTF16-H, UniGB-UTF16-H, UniGB-UTF32-H,
// UniGB-UTF8-H, UniJIS-UTF16-H and UniJIS-UTF16-V add mappings, and
// UniKS-UTF16-H holds three fewer). Identity-H and Identity-V are left out,
// since litho builds them, and Adobe-KR-UCS2, since the adobeucs2 resources
// already hold the identical file. Every other CMap veraPDF bundles maps
// exactly what PDF.js's copy maps.
var resources = []resource{
	{"Adobe-CNS1-7", "4143481ea7715202f2c4e6bf6e10472a404b289f56ab5af2a496f2fa6394323b"},
	{"Adobe-GB1-6", "4624d1d70b6142b8f700dddeb068362f25a9db4f725f35528c8be6c9523058f4"},
	{"Adobe-Japan1-7", "2252862cae73e8fa5fce7ded344443e3cfe1d2275601f0397c28b7ff727e7d36"},
	{"Adobe-Japan2-0", "e7c0a1a9eed5971c2b7cbfa9684d578a82facb14eb00e904e4fe2b885df417d1"},
	{"Adobe-KR-0", "ca58ef1abc2de732d5ff285ded96bb70700e293c19c416a89bae92a163037618"},
	{"Adobe-KR-1", "3b470c60b3d6196322be671571325b5025097fa97bedc51964948a7c6f927f7d"},
	{"Adobe-KR-2", "12d13fd0610ef3006add8169c59f4faaddc19c2629939417caaf70e9dfcec290"},
	{"Adobe-KR-3", "b922373bf0b75b6b8ab57bb5c7200e9ba10c9067a63561577739218692459603"},
	{"Adobe-KR-4", "6d7ebdae3b448592e18871ebd42eb62e4bd4e6c2a2846e7bbae6fdfcc7cede78"},
	{"Adobe-KR-5", "579d94396eafb215c694430bcd08e6fd612eadcdb53ca34a6f9f8e2ada378a74"},
	{"Adobe-KR-6", "6b8c3481b89d3aed99a19120ba365f649b2d82aacb1e15ec71c29697db97e9df"},
	{"Adobe-KR-7", "a6a76ebf76ace408bde2e69f04a17f47c3439e60100391297b10dbf7ec8b4574"},
	{"Adobe-KR-8", "ce6c2a24761aa8376e37a97c3c8740de1e84e9c7fea63a33265d91de97809a7e"},
	{"Adobe-KR-9", "ee0486530d138fbd4f3ae6c58f96cf5766dcd83eff72e39677d1ab5ec3906e10"},
	{"GBK2K-H", "7d945768331212eadca4a9c35390405efa72cf1be7f064cef170aa68cdab0be9"},
	{"UniAKR-UTF16-H", "f6f25740407e835538a8ec2bee34b82ee92969ba28771cf138fb2cc966998ef7"},
	{"UniCNS-UTF16-H", "602304a874f0cc40b463de73ca9d3d1c2d0e8d70cfcb2c9bd5bd124960d5aa00"},
	{"UniGB-UTF16-H", "3750279409a96fb3a8ce1fbc25eb95bab4ff7ade023e4e0df6bd33258a3a8726"},
	{"UniGB-UTF32-H", "a9c4dc0aa19eb30cf1d46c67edde25bae2e7177ffcfd55545d942155fe0f0d5e"},
	{"UniGB-UTF8-H", "e5e690511fe494a82fefc818a48fb5668704d46f346437562f3d16b29c61df4b"},
	{"UniJIS-UTF16-H", "5cf77ed38c25262dba845676738a887afe45e44425c3dd2489334c4d8c510fcb"},
	{"UniJIS-UTF16-V", "03bfb4e91452da537f347b8676e52adfa5dd10f8b920fa2cc34d343580354056"},
	{"UniKS-UTF16-H", "ddd37676e3ae5ced89062cd2c8c859dda4363f5a835c16aca989855f47b1f70c"},
}

// bundled lists every other CMap veraPDF 1.30.2 bundles, with the SHA-256 of
// its file at [veraPDFCommit]. The generator checks each one but copies none:
// PDF.js's copy maps the same codes, litho builds Identity-H and Identity-V,
// and Adobe-KR-UCS2 is the adobeucs2 file. Their names tell the conformance
// checks which predefined CMaps veraPDF resolves.
var bundled = []resource{
	{"83pv-RKSJ-H", "8a4d60015eb64baa6a73646a9983e7c6b987c0ab06e7c8b4b7e35a32a9ef1449"},
	{"90ms-RKSJ-H", "3e591fe1447fee58a8df9c4dcc52f42a134f014dc7884ebcf3cd64f5e02fcfe1"},
	{"90ms-RKSJ-V", "f61fb56f9360bcd43850cfa094f83e933d274bba5894d5edb6b4c5f42abefe63"},
	{"90msp-RKSJ-H", "824e53249249ebb3b67762cec9b814975b42d8311fa40795548ddf4fa79e4bec"},
	{"90msp-RKSJ-V", "603f62a6aef8d2af13b6107734f43ecc4e3973d635b16bcf4677798fbd5cf60d"},
	{"90pv-RKSJ-H", "560f4ee82591d5d3f818e094a15a9fd50a9285b11e5ae8735c6e07f10e2d0d24"},
	{"90pv-RKSJ-V", "f635a43f916d59c40f4f22d03f6a9f986ecfbe06e40de0bdb59eb52279f1c382"},
	{"Add-RKSJ-H", "633343db1a48217594859e4beaac76958cff002b533bfbe03c5aa6280d7bffce"},
	{"Add-RKSJ-V", "28ee457da75ee57462428e5aceb6a3b36f8e4a76da5ffcb809b02dfba1059600"},
	{"Adobe-CNS1-0", "4d0fbf58240e829be555656c69ff5035df3c9b96e65ace4b88a36da25cfb1cc5"},
	{"Adobe-CNS1-1", "def350b60edc58bd83eae5f0fd4042c5ee396c6439243a5694ee72aaa3a64350"},
	{"Adobe-CNS1-2", "5508dc9163640b54e5ccac4c27ce7c5c41517d4527f05b19598fe021e50883ba"},
	{"Adobe-CNS1-3", "81a5ea00f07f4a7b4d9889e2b5eba58b457e9abb894d8ae5e2a3de6e5ed189f7"},
	{"Adobe-CNS1-4", "e0baf6d32584ac942075cc56bea708bbe2472a78083c5fc3bca8b42c79afd237"},
	{"Adobe-CNS1-5", "552cac06aafa81418c70f0c5f2c9f9ed4d59152a72e981d20b7d51fdbef494e4"},
	{"Adobe-CNS1-6", "9190f32e01d5aed99dbaf5db9ee325b83eb8ab2fce4c336f821e2d9061994fc6"},
	{"Adobe-CNS1-UCS2", "8375afd535e153a7e3bcf448be93e5012b26229547a20d52520e46e3c386351f"},
	{"Adobe-GB1-0", "72509e2ace6c096d5ef0946e894edf8e77923d8004d519a06ff28a5e56d67c85"},
	{"Adobe-GB1-1", "b667f1b2e8773a3f05ef66177ad0bd2fc6a0adc470fa9de50f3a343bf041f97a"},
	{"Adobe-GB1-2", "3bc73a22b6e919733e7b5945d79c6c1393a930217f559c7b981cd06bd106661f"},
	{"Adobe-GB1-3", "fc5490ec245dda01c8b46797ff1429d70734355968db4d2eaffd9a4aae17086e"},
	{"Adobe-GB1-4", "270433b0479dd5ebed7f2ecff0ea481f1e3734f397893fb7e1b2d871486e206b"},
	{"Adobe-GB1-5", "ab3ce0efbaeefe12bc41711a276386989026c88bc4c3f81da3f33344f7df3421"},
	{"Adobe-GB1-UCS2", "368b40ec05568faf323cfbbddf23c60d566bbc88e366981eb6541b026381a3b8"},
	{"Adobe-Japan1-0", "c63388ab8d18d5cbbf9911b10329d057d60e1e9295b5644941e00fc8f8489452"},
	{"Adobe-Japan1-1", "9c93b77ac7de09ab1510e3d3b36c2ea64ab98d4b95938e5b5b83f1bf061ba391"},
	{"Adobe-Japan1-2", "51abc4a77a662306a66a1fadcb0d0e7526dbee73533b5aed266471177ee26076"},
	{"Adobe-Japan1-3", "b9241616bb96a3e76993787f986da7dcacf5e381ab79293e607864621ee1df16"},
	{"Adobe-Japan1-4", "43a9cdb72943d9522cd7d8a75a9b5f6674e3fa4e076b1df3792957619f089dd9"},
	{"Adobe-Japan1-5", "bb027d172491e320104b4d5ba8e4e9a80e3cadc15cc1cac97dc64de741ed202e"},
	{"Adobe-Japan1-6", "284f546fd6a6d41a4df764b00ccae5a62f4bd7c5f156140d45e23abc581f3acd"},
	{"Adobe-Japan1-UCS2", "69165b1e78b3083dde57766af80528882a37be5ee6600cdc02eaa1c9ff21bc4f"},
	{"Adobe-KR-UCS2", "6861a3208f331ecd73369c5d51eacf3b72c820fe3c87524f79c9ac1bb16830ec"},
	{"Adobe-Korea1-0", "6ef218726585b71e563a23d9978d47df58c40ca6fe7e77954af4c2ea5b28fd26"},
	{"Adobe-Korea1-1", "d1e84a360208a70d1eb50e2d751bfd9eaca7f2806365831839505744eb7ea096"},
	{"Adobe-Korea1-2", "b9ca3d49b1d36eb883fb878aecc88120ebb227f01a463866e2fc0b2aa6ca9ef0"},
	{"Adobe-Korea1-UCS2", "45bcf869acdcec2507f75836919e589e7137b9f9dbaa8e4975747035024248ef"},
	{"B5pc-H", "bdd5f880359612ec71fd3bee930ab4ee2a550fbe7193ae1a0cba727f3b37314b"},
	{"B5pc-V", "6ea096eb62f99ca54fdefc036bfb21058ba1664fc263411f5f7d3d3307e3c690"},
	{"CNS-EUC-H", "5bcb99e1829ce797c246462cf79c1e484223449355b4f91070046c7c804ef0eb"},
	{"CNS-EUC-V", "4e9dd5c2bac3283d7d582dc69c5b2fc1de612ae5d2ed2d257bf48a69c1b06bb5"},
	{"ETen-B5-H", "b885a24a86a0adee73c724b636facce5b33351155bc26adb320a797afee1effa"},
	{"ETen-B5-V", "c8ec6c4af760c07e3b46ed0d586f87a28278518f3adab17bbf78180e16c0bee9"},
	{"ETenms-B5-H", "98f7bc74c7accb0341398d2d949a4953528f25d38d070ba51d028da1bc3df40c"},
	{"ETenms-B5-V", "283856f9060d23ad6006cd84dbc22213ac70e83674d96288e3f35e8f2ba8bee9"},
	{"EUC-H", "02fc3b4034a9550084439ecf24a9cb2a057840cd4c20ff377a8245f0d7fa2f58"},
	{"EUC-V", "beaf35ca3bc2ad1bbe3d87a295f80393425c347bca7ad33e68958908f04079a9"},
	{"Ext-RKSJ-H", "ea4cbe2526a53486e48ab7acbb075e29fd8cdee2b056133610854af1510962ab"},
	{"Ext-RKSJ-V", "d943e0ae518c2bc1eea6b39dfcfab69c396bbdbdbf913c3c070f1cf3d83d54ae"},
	{"GB-EUC-H", "9c2b268c5fbcc9a23aa76d2f199c97fa9fbfcc0ef43f9f320bb367a6b0a47b20"},
	{"GB-EUC-V", "5acf07ba8e65be8aad1c7a4535574e41cb326abd295e9e5c1eb3e96037260397"},
	{"GB-H", "80e3b6caac7f23cb56e2b841464daefd9c32f14d902d67cfd8deba346ad2a35d"},
	{"GB-V", "8183888d47318ee30bf2b4f2a07aa953137ab43ac72df265c83f79f2b6180a62"},
	{"GBK-EUC-H", "6c88b308f122966de14696d819800bc89be2538fa14fa41f474e36b20cea11c6"},
	{"GBK-EUC-V", "73381a5bc2d0aacbf6b2ec211f34e042e7c74895342fcee073b0f9be2dbc8703"},
	{"GBK2K-V", "49e81ae0d4b7b35901fbaeff63558bb133d9d0398ac2d0e33fab75404ea71cd2"},
	{"GBKp-EUC-H", "d6ef0e6c393bd065f5b646b7dc87fe8df88f3608a9264fb5727b552418352d5a"},
	{"GBKp-EUC-V", "43c8fa12dcef0b1b584fdd077996fff388fc0e9a9d332e08556640ceefa9fa0e"},
	{"GBT-EUC-H", "21dca4424cff890670aee8ad6b045844d3b05ed879e87a79aab34bd7f722530b"},
	{"GBT-EUC-V", "100af66255a2f363059da703be769e6227c74e353df71329d1a18d83f5b808f2"},
	{"GBT-H", "ff770615091901ebfc9c2bfc39b8e7da4481ac38816e0849db149dac34f9ff86"},
	{"GBT-V", "04f4f5ddfd909d8a9f127957b3dcd549d121dba9b3459da396ae80abea94a0b1"},
	{"GBTpc-EUC-H", "274e76732cb5f12fe0081ac872a034b4adb8705a24221b5add479f4dd86efed2"},
	{"GBTpc-EUC-V", "f26971e59f62956eb6d8d27782c5c993cff89097ccab8e93ec682bf4b9302122"},
	{"GBpc-EUC-H", "ec6132762b28650eb0c8483ea9387af39acb9d8aca432cb1e4f79a1058e1cd40"},
	{"GBpc-EUC-V", "5fec79fd4f5f95d7638a150eec9b551268b8c29103446f0eee26866769ff384f"},
	{"H", "f90323c3fd16e107b02c888167739f210af7144beba1bda83949069535db3346"},
	{"HKscs-B5-H", "a9d484d9038eebfe056cab89af9304537b94cbe2c56031dbb64c1578391bc8d0"},
	{"HKscs-B5-V", "fc31fafb887a9af37eed4d1170d1ca6067140c2665b3214e95516a7ccbc7f1ac"},
	{"Identity-H", "866c40836b501ddc098d4077d8d31e8ab8f4b9f9842db91ea9acf87488e6d7c1"},
	{"Identity-V", "1c5ae69b98af36325cc8539f4cbe4d041f8b55b497b1e9ea45b3a75c484ed5c2"},
	{"KSC-EUC-H", "4aa1c382cc5c291e9af14db1a35f8e1a5ddc8507420fdcc16dfb8bd0d9537184"},
	{"KSC-EUC-V", "963ae7419ae0c87e2d9b56bda40446af4e46453f27a4384bc8b3e8106fee51b4"},
	{"KSCms-UHC-H", "94ea4aeb4aff7b75dea32fa5425fef6f9a2859d99d001e874fb7948f672ffbb2"},
	{"KSCms-UHC-HW-H", "317ddb148acf62c396f9610ddabcccb7fb4d8035af247c60184ae8dcaa969eff"},
	{"KSCms-UHC-HW-V", "d3628080a1e9ea9ba388fc65c3d2ebd80e7f297b3916495ecc57e977dfdbd10f"},
	{"KSCms-UHC-V", "eb999e7aedff28d89a21c6c3f57d4450dd68054a1f40fa39e0259afad3a6e5cc"},
	{"KSCpc-EUC-H", "43f20e1fc1b09caf46e0756ace2ac4ee8fb9380c32ab83f18dc2eb608c84c92b"},
	{"KSCpc-EUC-V", "4fbf738a8f5a2509d576b76b1c0f4a025ee30ceb0918f318098ae55e4aa8870a"},
	{"UniCNS-UCS2-H", "66d61241fc5753ad3552a323cb9a19df13d7fe8f20d93b4ff3ffdbd612f95c66"},
	{"UniCNS-UCS2-V", "bcab0409b46fbe543702385af21fcb157a3a48dfdf1c8339b9c207cd475c749a"},
	{"UniCNS-UTF16-V", "a3d2f8d74db8745d5b463a159ac53391f3d2950a8bcf1fb0d7bf0cfaa1395302"},
	{"UniGB-UCS2-H", "d2b100a68c016190347c6e0ecee3e7d6d1accd7395d7e803bf88b7ffef156725"},
	{"UniGB-UCS2-V", "d94d2e56a9a5cb89e0a68c31eba1ff40aa604b3a504be844c67de64af64eec96"},
	{"UniGB-UTF16-V", "cc9608b7c8c11133c1148eda883c10433e1d0dc2c964f41744eae89fe75f77d3"},
	{"UniGB-UTF32-V", "16ca3b858de179df9babbc4e291deb2b53d26813ab69e16ade4091c88fccacea"},
	{"UniGB-UTF8-V", "fd506698da24dd81b389ef0011d4436af07f264259044e0c76a4f1ab48b4fa4a"},
	{"UniJIS-UCS2-H", "25a5fa588b689c13501498d08d34866b77c42582e6299a4e223849db9281e86b"},
	{"UniJIS-UCS2-HW-H", "3008e520b23f8d3838f2463efb1a9842f66adca29cbecf15cd6bda1955788e57"},
	{"UniJIS-UCS2-HW-V", "45d7bd6e0cbf31853e3b517d5b6d2939e0831d86ca4318887a059967b9512545"},
	{"UniJIS-UCS2-V", "cf815152be6bee432087f63fba26345b581d718c7ce5ba4560fd6ebde8f2ad2a"},
	{"UniKS-UCS2-H", "bd4277d5ae23f3a92252cc54e6449c240d25c918c3b91f6abbccb1a2be4b9acf"},
	{"UniKS-UCS2-V", "904f9c063ab89075b09bf2c471cfc2ac4db6604c3a2ae29a08c9c37761a353e4"},
	{"UniKS-UTF16-V", "334ae271967e8bc786efd35283c615e99c23052b43e15039cc656b8954ee2a18"},
	{"V", "600c022cc51273df858da76db284e924053615378a418f4cd5b3116ca0be1d51"},
}

// license is Adobe's BSD 3-Clause license of cmap-resources, which every
// file also carries in its header, copied beside the resources.
var license = resource{licenseSource, "742665db9c8e1bc72603c6d319ca3e90b83bd6d95202f0cd4ef11068a07a9c29"}

func main() {
	src := flag.String("src", "", "read the files from this directory instead of downloading them at the pinned commits")
	out := flag.String("out", "", "cmap package directory (default: pkg/internal/typography/cmap)")
	flag.Parse()

	pkgDir := *out
	if pkgDir == "" {
		pkgDir = findCMapDir()
	}
	dataDir := filepath.Join(pkgDir, outDirName)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		fatalf("creating %s: %v", dataDir, err)
	}

	for _, r := range resources {
		data, err := fetch(*src, veraPDFRawURL+"/"+veraPDFCommit+"/"+veraPDFDir+"/"+r.Name, r)
		if err != nil {
			fatalf("%v", err)
		}
		gz, err := compress(data)
		if err != nil {
			fatalf("compressing %s: %v", r.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, r.Name+".gz"), gz, 0o600); err != nil {
			fatalf("writing %s: %v", r.Name, err)
		}
	}
	for _, r := range bundled {
		if _, err := fetch(*src, veraPDFRawURL+"/"+veraPDFCommit+"/"+veraPDFDir+"/"+r.Name, r); err != nil {
			fatalf("%v", err)
		}
	}
	lic, err := fetch(*src, adobeRawURL+"/"+adobeCommit+"/"+licenseSource, license)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, licenseName), lic, 0o600); err != nil {
		fatalf("writing %s: %v", licenseName, err)
	}
	code, err := generate()
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, outGoName), code, 0o600); err != nil {
		fatalf("writing %s: %v", outGoName, err)
	}
	fmt.Printf("Generated %s and %s/ from %s at %s\n", outGoName, outDirName, veraPDFRepo, veraPDFCommit)
}

// fetch returns the file at url, read from dir under its base name when dir
// is set and downloaded otherwise, after checking it against r's digest.
func fetch(dir, url string, r resource) ([]byte, error) {
	var data []byte
	if dir != "" {
		local := filepath.Join(dir, r.Name)
		b, err := os.ReadFile(local) //nolint:gosec // the directory is the developer's own command-line flag
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", local, err)
		}
		data = b
	} else {
		b, err := download(url)
		if err != nil {
			return nil, err
		}
		data = b
	}
	if err := verify(data, r); err != nil {
		return nil, err
	}
	return data, nil
}

// download returns the body at url, refusing one past [maxResourceBytes].
func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // the URL is built from package constants
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(data) > maxResourceBytes {
		return nil, fmt.Errorf("downloading %s: %w", url, errTooLarge)
	}
	return data, nil
}

// verify reports whether data is the file r pins.
func verify(data []byte, r resource) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != r.SHA256 {
		return fmt.Errorf("%s: %w: got %s, want %s", r.Name, errDigestMismatch, got, r.SHA256)
	}
	return nil
}

// compress gzips data with a zero header, so the output depends on the input
// and the compressor alone.
func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("creating gzip writer: %w", err)
	}
	if _, err := zw.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// generate renders the gofmt-formatted Go file recording where the resources
// come from and their digests.
func generate() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by " + generatorLabel + "; DO NOT EDIT.\n\n")
	buf.WriteString(comment(fmt.Sprintf("The %s directory holds gzip copies of encoding CMaps veraPDF "+
		"1.30.2 bundles, taken from %s of %s, commit %s, unchanged. They are Adobe's CMap "+
		"resources (%s). Copyright Adobe, licensed under the BSD 3-Clause License, which each "+
		"file carries in its header: see %s/%s.", outDirName, veraPDFDir, veraPDFRepo, veraPDFCommit,
		adobeRepo, outDirName, licenseName)))
	buf.WriteString("\npackage cmap\n\n")
	buf.WriteString(comment("veraPDFCMapDigests maps each CMap the " + outDirName + " directory holds to the " +
		"SHA-256 of the file as veraPDF publishes it, before compression."))
	buf.WriteString("var veraPDFCMapDigests = map[string]string{\n")
	for _, r := range resources {
		fmt.Fprintf(&buf, "\t%q: %q,\n", r.Name, r.SHA256)
	}
	buf.WriteString("}\n\n")
	buf.WriteString(comment("veraPDFBundledCMaps holds the name of every CMap veraPDF 1.30.2 bundles, " +
		"those the " + outDirName + " directory holds included."))
	names := make([]string, 0, len(resources)+len(bundled))
	for _, r := range resources {
		names = append(names, r.Name)
	}
	for _, r := range bundled {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	buf.WriteString("var veraPDFBundledCMaps = map[string]bool{\n")
	for _, n := range names {
		fmt.Fprintf(&buf, "\t%q: true,\n", n)
	}
	buf.WriteString("}\n")
	code, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w", err)
	}
	return code, nil
}

// commentWidth is the column the generated comments wrap at.
const commentWidth = 76

// comment renders text as "//" comment lines wrapped at [commentWidth].
func comment(text string) string {
	var b strings.Builder
	line := "//"
	for word := range strings.FieldsSeq(text) {
		if len(line) > len("//") && len(line)+1+len(word) > commentWidth {
			b.WriteString(line + "\n")
			line = "//"
		}
		line += " " + word
	}
	b.WriteString(line + "\n")
	return b.String()
}

func findCMapDir() string {
	for _, c := range []string{".", "pkg/internal/typography/cmap"} {
		if _, err := os.Stat(filepath.Join(c, "usecmap.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find the cmap package directory; run from the repo root or pkg/internal/typography/cmap/")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-verapdf-cmaps: "+format+"\n", args...)
	os.Exit(1)
}
