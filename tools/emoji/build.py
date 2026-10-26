# Crea un font emoji CBDT con le stesse emoji (glifi, sequenze, regole) di fonts/PingEmoji.ttf
# ma con le immagini di un altro set, a 72 ppem. Le emoji che il set non ha restano quelle di Google.
#
#   pip install fonttools pillow cairosvg uharfbuzz
#   git clone --depth 1 https://github.com/microsoft/fluentui-emoji     (Fluent Emoji, MIT)
#   npm pack @twemoji/svg && tar xzf twemoji-svg-*.tgz                   (Twemoji, CC-BY 4.0)
#   FLUENT=fluentui-emoji/assets TWEMOJI=package python3 build.py fluent3d fonts/emoji/v1/fluent3d.ttf
#
#   set: fluent3d | fluentflat | twemoji
# Dopo aver rigenerato un font va aggiornata la sua impronta SHA-256 in emoji.go (e conviene
# usare una nuova cartella, es. fonts/emoji/v2, perché le versioni già installate cercano la v1).
import sys, os, io, json, glob
from multiprocessing import Pool
from fontTools.ttLib import TTFont
from PIL import Image
import cairosvg

TEMPLATE = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '..', 'fonts', 'PingEmoji.ttf')
FLUENT = os.environ.get('FLUENT', 'fluentui-emoji/assets')
TWEMOJI = os.environ.get('TWEMOJI', 'package')
PPEM0, PPEM = 109, 72          # dimensione del font originale e di quello nuovo (immagini più piccole)
K = PPEM / PPEM0
W, H = round(136 * K), round(128 * K)
BOX = round(122 * K)

def norm(seq):
    return '-'.join(c for c in seq.lower().replace(' ', '-').split('-') if c and c != 'fe0f')

def index_twemoji():
    return {norm(os.path.basename(p)[:-4]): p for p in glob.glob(TWEMOJI + '/*.svg')}

TONES = {'Light': '1f3fb', 'Medium-Light': '1f3fc', 'Medium': '1f3fd', 'Medium-Dark': '1f3fe', 'Dark': '1f3ff'}

def index_fluent(kind):  # kind: '3D' o 'Flat'
    idx = {}
    for meta in glob.glob(FLUENT + '/*/metadata.json'):
        d = os.path.dirname(meta)
        m = json.load(open(meta))
        base = m.get('unicode', '')
        files = glob.glob(f'{d}/{kind}/*')
        if files:
            idx[norm(base)] = files[0]
        default = glob.glob(f'{d}/Default/{kind}/*')
        if default:
            idx.setdefault(norm(base), default[0])
        for tone, cp in TONES.items():
            tf = glob.glob(f'{d}/{tone}/{kind}/*')
            if not tf:
                continue
            # la sequenza con il tono è tra quelle elencate nei metadati
            for s in m.get('unicodeSkintones', []):
                if cp in s.split():
                    idx[norm(s)] = tf[0]
                    break
    return idx

def load(path):
    if path.endswith('.svg'):
        data = cairosvg.svg2png(url=path, output_width=512, output_height=512)
        im = Image.open(io.BytesIO(data))
    else:
        im = Image.open(path)
    return im.convert('RGBA')

def render(path):
    im = load(path)
    bb = im.getchannel('A').point(lambda a: 255 if a > 8 else 0).getbbox()
    if bb:
        im = im.crop(bb)
    im.thumbnail((BOX, BOX), Image.LANCZOS)
    canvas = Image.new('RGBA', (W, H), (0, 0, 0, 0))
    canvas.paste(im, ((W - im.width) // 2, (H - im.height) // 2), im)
    q = canvas.quantize(colors=256, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.NONE)
    buf = io.BytesIO()
    q.save(buf, 'PNG', optimize=True)
    return buf.getvalue()

def shrink(data):  # immagine di Google ridotta alle misure nuove (emoji mancanti nel set)
    im = Image.open(io.BytesIO(data)).convert('RGBA').resize((W, H), Image.LANCZOS)
    q = im.quantize(colors=256, method=Image.Quantize.FASTOCTREE, dither=Image.Dither.NONE)
    buf = io.BytesIO()
    q.save(buf, 'PNG', optimize=True)
    return buf.getvalue()

def job(args):
    name, path = args
    try:
        if isinstance(path, bytes):
            return name, shrink(path)
        return name, render(path)
    except Exception as e:
        return name, None

def main():
    kind, out = sys.argv[1], sys.argv[2]
    idx = {'twemoji': index_twemoji, 'fluent3d': lambda: index_fluent('3D'),
           'fluentflat': lambda: index_fluent('Flat')}[kind]()
    # quale glifo del font originale disegna ciascuna emoji del nuovo set: lo dice HarfBuzz
    import uharfbuzz as hb
    blob = hb.Blob.from_file_path(TEMPLATE)
    hbfont = hb.Font(hb.Face(blob))
    f = TTFont(TEMPLATE)
    order = f.getGlyphOrder()
    strike = f['CBDT'].strikeData[0]
    def glyph_of(cps):
        buf = hb.Buffer()
        buf.add_codepoints(cps)
        buf.guess_segment_properties()
        hb.shape(hbfont, buf)
        gl = [order[i.codepoint] for i in buf.glyph_infos if order[i.codepoint] != 'space']
        return gl[0] if len(gl) == 1 else None
    chosen = {}
    for k, p in idx.items():
        cps = [int(c, 16) for c in k.split('-')]
        g = glyph_of(cps)
        if g is None:  # con il selettore di variante (FE0F) dopo ogni simbolo
            alt = []
            for c in cps:
                alt.append(c)
                if c < 0x1F000 and c not in (0x200D,):
                    alt.append(0xFE0F)
            g = glyph_of(alt)
        if g and g in strike and g not in chosen:
            chosen[g] = p
    work = list(chosen.items())
    missing = [n for n in strike.keys() if n not in chosen]
    work += [(n, strike[n].imageData) for n in missing]
    with Pool() as pool:
        done = pool.map(job, work, chunksize=20)
    ok = 0
    for name, data in done:
        g = strike[name]
        g.ensureDecompiled()  # altrimenti al salvataggio si riscrivono i dati originali
        if data:
            g.imageData = data
            ok += 1
        m = g.metrics
        m.width, m.height = W, H
        m.BearingX, m.BearingY, m.Advance = round(m.BearingX * K), round(m.BearingY * K), round(m.Advance * K)
    ok -= len(missing)
    bs = f['CBLC'].strikes[0].bitmapSizeTable
    bs.ppemX = bs.ppemY = PPEM
    for lm in (bs.hori, bs.vert):
        lm.ascender, lm.descender, lm.widthMax = round(lm.ascender * K), round(lm.descender * K), round(lm.widthMax * K)
    f.save(out)
    print(f'{kind}: sostituite {ok}/{len(strike)} immagini, mancanti {len(missing)} (restano quelle di Google), {os.path.getsize(out)/1e6:.1f} MB')

main()
