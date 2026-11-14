package page

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// pinnedRenderHashes holds the SHA-256 of the float canvas of each fixture
// of [TestRender_SameBytesOnEveryArchitecture]. Every architecture must
// give these bytes: a change here is a change of output.
var pinnedRenderHashes = map[string]string{
	"alpha 173":                "c9055ad1723527bd62689e6add0001a2d27d60a12627407fdec1cd55a5d3f5ab",
	"alpha 72":                 "056ed6b8e38150e718d7872c1e2b82f4f955f01e5d8beeca43b76c0bbce98b98",
	"annot_flags 173":          "5cc171bcfee22a4410d5ef85a6a2ebd076e6ba65878a9bc2e3e680ae7ce795a5",
	"annot_flags 72":           "aadec957d6a380b51c059dbc605802ea7e37dc38027e39513fd59ea04d417a55",
	"annot_matrix 173":         "c093641f5ee9367f02c7b963fbe4142853af87be6420e8e787098d8b2a45bd70",
	"annot_matrix 72":          "e0c9024145bb54fdc7b9f1c66a4804251a31b7839edd83d310a8aa990116b3df",
	"annot_norotate 173":       "a856e055cf279022550b46e8f6aedc07fa6801b777b5c58c2c7910eb7c26ed3d",
	"annot_norotate 72":        "686ca3314aa940a5c584fda646c0dc125d74053277292926d81d65a3b28c0c6f",
	"annot_synth_geometry 173": "013baa78e0603af372545c0bbca39d4035617e945254ecd651c7e60b08049d6b",
	"annot_synth_geometry 72":  "cd1821ed3f8a31264cbe07abe86285db420b812f812ee4f3ab4c28e8127e1cf2",
	"annot_synth_lines 173":    "39aa026378db132ee246a1916f46b1830755a4a413be4c61ee98d33b4416ad64",
	"annot_synth_lines 72":     "4e3abcb47b79289c88a0d3c09ba0d1f89d2c02f6b4a1a6b0d088ae7f58bfb922",
	"annot_synth_markup 173":   "78616ea7bd28d766ebe364218eb216094d3632aa0093f2bf8bdbc295b195abe3",
	"annot_synth_markup 72":    "993a0699354caf99d9b5f31d3fe781bff28d69fbd321d0a0871011cef5eba1fa",
	"blend_modes 173":          "64282ae240de71255f481c6cc4413db6e7e6d1791e39cf855c2ea7dacc0b259f",
	"blend_modes 72":           "c4894d7d242b1a5a798e096f74513f872d47f93eb69120ca134444f3df24bd3f",
	"clips 173":                "e8dc219ca60ee95fdcafee24ee6c9872ec680a4224099ae664b1abdc17ac2c17",
	"clips 72":                 "6d31293839ae962f1890f29a6867b4d0484f6c4f694798394c4e767c095eb293",
	"fill_rules 173":           "c38d31cc9e25d0fc56f24db49ab5a6eef0811986157175603bb7f87b6149d5fb",
	"fill_rules 72":            "38382985d434ee0db2fca50c84b832bfc6ab8cbd040c81d8226e34d0f04a66f6",
	"forms 173":                "a207f8212caf1142c58f046adfc4f7b540c8b7159599f75acb83d035c2436c52",
	"forms 72":                 "4402e802b26798bc3ec72a875161a4934e72bf47e684763504c5fefbda5ba576",
	"groups 173":               "1cfc206f0e2d41722c53d45db1522fed2047a117a27101cba00a87f1af74665f",
	"groups 72":                "570e6fc5fcdc1fe28a24fe78802143fb8961eb844ba4f7db325cbfcc4b0ad0ce",
	"images 173":               "f6bdc2fc3cee6586e3b14c35a2b5073f6b121e5d8292faab2d5bf957f9338dc9",
	"images 72":                "a09137f31369c0f629cbc7ab892288fa7b14216c70cbde9dfd7fe12b383c81d5",
	"knockout_alpha 173":       "ff20098cb62b9bbbfe79219b886910d3a788af97ee30faef24c972b3e9296d41",
	"knockout_alpha 72":        "d9816782636f55cead77122fafab340a5e6d835626ef3dd0cd543b5af4c8aa0a",
	"nested_groups 173":        "b8c1a33d007cc8216f8dde68aa7c20bc91c529435d5d2cd2a9cd3de041a9f55c",
	"nested_groups 72":         "0cc8e081dfd8f7596e7ed54ba41ebc43c1a310ab0e4bc2f763417c523c1e5db4",
	"rotated 173":              "d848fd84e1fc48497f0e227f0c56c502dd663e4bed1191624528afddf09652d2",
	"rotated 72":               "2d0abe57897ba512c190e266ddd7f7b62a402e56dcdd655a2c646d432ceb9d1a",
	"shadings 173":             "a4f6e4f2826393a0820b91e695d61a0ea37922a7bbade35dc6152c29b6d0b4cb",
	"shadings 72":              "109de80ce53bee72f4d1a3dfb7f2be91e2712f55cf7c8c60b821099bcac4a9ab",
	"smask_alpha 173":          "e96f8cb49bb262c866e062e3526c58cc76d6408f9d9e0cf1119eb9ecf59f3104",
	"smask_alpha 72":           "7b0b6a8f63a59beebdbb2c548db466d8585a096f179b97e7e822a48c5d4515d1",
	"smask_image 173":          "b4d036990428d2d3fcfe71534b382d10b56788054b318db61450d1833dcbfe1f",
	"smask_image 72":           "1120d6968ff5682ffc6052f3840ab6a941fd7d1f8805f0ef85840b3bf59744ba",
	"smask_luminosity 173":     "3f49294edfa6153aaded40801ed947dfbd1fde275e1a9e9b9e7d31f7b0e9a0e9",
	"smask_luminosity 72":      "40ffd4936c6b9d274b4976ff0bd05509fa4e21c316a4d165ce0f86a405afe20c",
	"strokes 173":              "9f936657ab9072881b17b4cdd8bf706063481b72f257a751fbb52caf8e467290",
	"strokes 72":               "d10d9f5b920a8f43dd29dc063b7c022e724802b2be5f86a83a1275cf8063b853",
	"text 173":                 "d356b34829bb853f2bfcbb2eb76595158750d154f5ecb6a02b8eb13dc6275153",
	"text 72":                  "3154d021e05966830ccde4c118b7672eb0781a7f769c0d914c779dd3eed1e652",
	"tiling_coloured 173":      "28a905735731d5f31d54810a4c58b9d6226105faf71e6346c5f942c7fbb73203",
	"tiling_coloured 72":       "8c578016a7091e891328e334b0c049beab52abc1e08457fa4f898b2cefe5d607",
	"tiling_form 173":          "10cce62f09dd9ffa5c99f3ca14a6c53578d681dcb0e48873e1f7f0c082c22d76",
	"tiling_form 72":           "d35189b74f9be6cf76d4a62a0d1279ba371bc73f60dbc896a37905f06510f2d6",
	"tiling_imagemask 173":     "44822fadcec8838a4ea52a898bd5cb2465d05fa41cb277f805f3b092657d47f3",
	"tiling_imagemask 72":      "69f5c3fdf461ec34faee62ce49e191a5160742aa8f1f233ad6f8ddb68db20a4c",
	"tiling_negative 173":      "2ed13fc75269faf21362cd35f5cc023f5ace12a6946584e688d54321a85a2734",
	"tiling_negative 72":       "a23ba8220ef1108c67308af46df9c1ce6f3db387a090a0ce2ee4dfd9e60bd61a",
	"tiling_nested 173":        "96efbc097d5e89eae632a78240e2412a3b8c78f4c177a86b86bc42203342e45b",
	"tiling_nested 72":         "dcdecd35b6987e7cbc70294ed11d7077719eb041502d03fc11078cfaad93dfb2",
	"tiling_uncoloured 173":    "797799329a7df03d6a8aabeca097ea4d4a566b145feadfcfa228f44279518dd9",
	"tiling_uncoloured 72":     "7e484c39125e8c0a04d97a91b1174b5e32f22847004dbac4ffca8c3889206d1c",
	"type3 173":                "5764ba912313e7ed8f32ef33a244f9ffede65e41e79a88e91ad8bfa679ecb2b7",
	"type3 72":                 "92fa847db0a1878d16781560baaf9393801170a2f7be77f769ab458d4920655e",
	"vertical 173":             "27a5819b840ae26ec3e3f941a803fe50c02774f3736a1d463e44b330f291a488",
	"vertical 72":              "718e9061688d50b2792199669b4c6df9e5b9f7b45c6d410f2608f163938044d5",
}

// TestRender_SameBytesOnEveryArchitecture renders the fixtures of the
// renderer's poppler, transparency, pattern and annotation tests at two
// resolutions over a transparent background and compares the SHA-256 of
// each float canvas with the hash pinned for it. The renderer rounds every
// product on its own before it is summed, so arm64, which fuses a product
// with a sum into one instruction unless told otherwise (The Go
// Programming Language Specification, Arithmetic operators), draws the
// same bytes as amd64 and 386.
func TestRender_SameBytesOnEveryArchitecture(t *testing.T) {
	t.Parallel()
	sets := []map[string]fixture{popplerFixtures, transparencyFixtures, patternPopplerFixtures, annotPopplerFixtures, synthPopplerFixtures}
	all := map[string]fixture{}
	for _, set := range sets {
		maps.Copy(all, set)
	}
	for _, name := range slices.Sorted(maps.Keys(all)) {
		f := all[name]
		if f.substitute != nil {
			continue
		}
		doc, pg := open(t, f.bytes(), limits.Limits{})
		for _, dpi := range []float64{72, 173} {
			opts := f.options()
			opts.DPI, opts.Background = dpi, [4]float32{}
			c, _, err := Render(context.Background(), doc, pg, opts)
			if err != nil {
				t.Fatalf("%s at %v dpi: %v", name, dpi, err)
			}
			raw := make([]byte, 0, 4*len(c.Pix))
			for _, v := range c.Pix {
				raw = binary.LittleEndian.AppendUint32(raw, math.Float32bits(v))
			}
			sum := sha256.Sum256(raw)
			key := fmt.Sprintf("%s %v", name, dpi)
			if got := hex.EncodeToString(sum[:]); got != pinnedRenderHashes[key] {
				t.Errorf("%q: sha256 %s, pinned %s", key, got, pinnedRenderHashes[key])
			}
		}
	}
}
