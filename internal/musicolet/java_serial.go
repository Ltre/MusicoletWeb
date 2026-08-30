package musicolet

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// canonicalJavaSerialization is intentionally a read-only token decoder. It
// never loads classes or instantiates Java objects; it only recognizes stream
// tags and their self-delimiting payloads, leaving unknown object payload as
// bounded hex chunks.
func canonicalJavaSerialization(b []byte) (string, error) {
	if len(b)<4||binary.BigEndian.Uint16(b[:2])!=0xaced||binary.BigEndian.Uint16(b[2:4])!=5{return "",fmt.Errorf("not Java serialization stream")}
	var out strings.Builder;out.WriteString("JAVA_SERIALIZATION STREAM_VERSION=5\n")
	for i:=4;i<len(b);{off:=i;tc:=b[i];i++;switch tc{
	case 0x70:fmt.Fprintf(&out,"%08x TC_NULL\n",off)
	case 0x71:if i+4>len(b){return "",fmt.Errorf("truncated TC_REFERENCE")};h:=binary.BigEndian.Uint32(b[i:i+4]);i+=4;fmt.Fprintf(&out,"%08x TC_REFERENCE 0x%08x\n",off,h)
	case 0x74:if i+2>len(b){return "",fmt.Errorf("truncated TC_STRING")};n:=int(binary.BigEndian.Uint16(b[i:i+2]));i+=2;if i+n>len(b){return "",fmt.Errorf("truncated TC_STRING data")};v:=string(b[i:i+n]);i+=n;fmt.Fprintf(&out,"%08x TC_STRING %s\n",off,strconv.QuoteToASCII(v))
	case 0x7c:if i+8>len(b){return "",fmt.Errorf("truncated TC_LONGSTRING")};n:=binary.BigEndian.Uint64(b[i:i+8]);i+=8;if n>uint64(len(b)-i){return "",fmt.Errorf("truncated TC_LONGSTRING data")};v:=string(b[i:i+int(n)]);i+=int(n);fmt.Fprintf(&out,"%08x TC_LONGSTRING %s\n",off,strconv.QuoteToASCII(v))
	case 0x77:if i>=len(b){return "",fmt.Errorf("truncated TC_BLOCKDATA")};n:=int(b[i]);i++;if i+n>len(b){return "",fmt.Errorf("truncated block data")};fmt.Fprintf(&out,"%08x TC_BLOCKDATA %s\n",off,hex.EncodeToString(b[i:i+n]));i+=n
	case 0x7a:if i+4>len(b){return "",fmt.Errorf("truncated TC_BLOCKDATALONG")};n:=int(binary.BigEndian.Uint32(b[i:i+4]));i+=4;if n<0||i+n>len(b){return "",fmt.Errorf("truncated long block data")};fmt.Fprintf(&out,"%08x TC_BLOCKDATALONG %s\n",off,hex.EncodeToString(b[i:i+n]));i+=n
	case 0x72:fmt.Fprintf(&out,"%08x TC_CLASSDESC\n",off)
	case 0x73:fmt.Fprintf(&out,"%08x TC_OBJECT\n",off)
	case 0x75:fmt.Fprintf(&out,"%08x TC_ARRAY\n",off)
	case 0x76:fmt.Fprintf(&out,"%08x TC_CLASS\n",off)
	case 0x78:fmt.Fprintf(&out,"%08x TC_ENDBLOCKDATA\n",off)
	case 0x79:fmt.Fprintf(&out,"%08x TC_RESET\n",off)
	case 0x7b:fmt.Fprintf(&out,"%08x TC_EXCEPTION\n",off)
	case 0x7d:fmt.Fprintf(&out,"%08x TC_PROXYCLASSDESC\n",off)
	case 0x7e:fmt.Fprintf(&out,"%08x TC_ENUM\n",off)
	default:j:=i;for j<len(b)&&j-i<32&&b[j]<0x70{j++};chunk:=b[off:j];if len(chunk)==0{chunk=[]byte{tc}};if utf8.Valid(chunk)&&isMostlyPrintable(chunk){fmt.Fprintf(&out,"%08x DATA %s\n",off,strconv.QuoteToASCII(string(chunk)))}else{fmt.Fprintf(&out,"%08x DATA_HEX %s\n",off,hex.EncodeToString(chunk))};if j>i{i=j}}
	}
	return out.String(),nil
}
func isMostlyPrintable(b []byte)bool{if len(b)==0{return false};good:=0;for _,r:=range string(b){if r=='\n'||r=='\r'||r=='\t'||r>=0x20{good++}};return good*4>=len([]rune(string(b)))*3}
