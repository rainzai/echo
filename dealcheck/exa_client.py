import json
import re
from datetime import date
from urllib.parse import urlparse
from dotenv import load_dotenv
from exa_py import Exa

load_dotenv()
exa = Exa()

COLOURS = ["", "black", "white", "grey", "silver", "blue", "green", "red", "pink", "purple",
           "yellow", "orange", "brown", "beige", "gold", "other"]
CONDITIONS = ["new", "refurbished", "used"]
SCHEMA = {
    "type": "object",
    "properties": {
        "product_name": {"type": "string"},
        "price": {"type": "number", "description": "The price you pay right now to buy this product new from this shop: "
                  "the main price shown with the product's title and buy button. "
                  "Not the crossed-out, list or advice price (adviesprijs), not refurbished or second-hand "
                  "(Tweedekans), not other variants, bundles, accessories or recommended products. "
                  "A comma can be the decimal sign, and ',-' means no cents. "
                  "0 if the page text doesn't show this product's price."},
        "currency": {"type": "string", "description": "ISO currency code, e.g. EUR"},
        "was_price": {"type": "number", "description": "The higher old price the page shows with the current price "
                      "to claim a discount on this product: crossed-out, 'was', list or advice price. "
                      "0 if the page shows none."},
        "discount_percent": {"type": "number", "description": "The discount percentage the page shows for this product, "
                             "as a positive number. 0 if the page shows none."},
        "model": {"type": "string", "description": "The manufacturer's model name as in the product title, without colour "
                  "or region codes, e.g. WH-1000XM6. Not the shop's own article or item number. Empty if not shown."},
        "colour": {"type": "string", "enum": COLOURS, "description": "The colour of this exact product. Empty if none."},
        "size": {"type": "string", "description": "Storage, size or capacity of this exact product, e.g. 256gb, 46mm or 6.2l. "
                 "Empty if none."},
        "shop_page": {"type": "boolean", "description": "True only if this page is one shop's own page for this one product, "
                      "where you buy it from that shop. False for reviews, news, price comparison sites that list "
                      "offers from several shops, and category, search or list pages."},
        "condition": {"type": "string", "enum": CONDITIONS, "description": "Condition of the item this page sells: "
                      "refurbished for refurbished, renewed, graded or Tweedekans items, used for second-hand items "
                      "from a person, new otherwise. Side offers shown on the page don't count."},
        "bundle": {"type": "boolean", "description": "True if this page sells the product together with other products, "
                   "e.g. a console with extra games or a set with extra accessories. False for the product on its own."},
    },
    "required": ["product_name", "price", "currency", "was_price", "discount_percent", "model", "colour", "size", "shop_page",
                 "condition", "bundle"],
}
PRICE_QUESTION = ("What does this shop charge right now for the product this page is about? "
                  "Use the main price next to the product title and buy button. "
                  "Ignore refurbished, second-hand, outlet or Tweedekans offers shown on the page, "
                  "unless the page itself sells that refurbished or second-hand item. "
                  "Ignore crossed-out, list or advice prices, other variants, bundles and recommended products.")
LIVE_TIMEOUT = 30000
# other shops are extras, don't wait long for them
OTHERS_TIMEOUT = 10000
CONTENTS = {"text": True, "summary": {"query": PRICE_QUESTION, "schema": SCHEMA}}

# matches 1.299,00 and 1,299.00 and 319,-
def price_pattern(price):
    whole, cents = divmod(round(price * 100), 100)
    groups = re.findall(r"\d{1,3}(?=(?:\d{3})*$)", str(whole))
    return r"(?<!\d)" + r"[.,  ]?".join(groups) + (rf"\D{{0,3}}{cents:02d}" if cents else "") + r"(?!\d)"

def in_text(price, text):
    return re.search(price_pattern(price), text) is not None

# words shops put before an old price, in the languages we search
WAS_WORDS = re.compile(r"(adviesprijs|\bvan|\bwas|\brrp|\buvp|\bovh|voorheen|normaal|list price|"
                       r"reduced from|\bstatt|\bnorm|ohjehinta|\bennen)[\s:.€£]*$", re.I)

# a was price only counts when the page marks it as one, so a price from a carousel or review doesn't
def marked_was(was, price, text):
    percent = round((was - price) / was * 100)
    shown = percent >= 2 and any(abs(int(p) - percent) <= 1 for p in re.findall(r"(?<!\d)(\d{1,2})\s?%", text))
    return shown or any(WAS_WORDS.search(text[max(0, m.start() - 25):m.start()])
                        for m in re.finditer(price_pattern(was), text))

COMPARE_WORDS = re.compile(r"vergelijk|compare|comparison|preisvergleich|vertaa|hintavertailu", re.I)

# a page naming 3 or more other shops' domains, or saying so in its title or link, compares shops, it isn't one
# only the title, because shop pages have a "compare" button in their menu
def compares_shops(text, url):
    if COMPARE_WORDS.search(url + " " + text.strip().split("\n")[0]):
        return True
    own = shop(url).split(".")[0]
    domains = set(re.findall(r"\b[a-z0-9-]+\.(?:nl|be|de|fi|fr|co\.uk|com)\b", text.lower()))
    return len({d for d in domains if own not in d}) >= 3

# page text without "key":value data blocks, so only what a shopper sees
def visible(text):
    return re.sub(r'"[\w@$-]+"\s*:\s*("[^"]*"|[\d.]+)', " ", text)

# the shop's own product data, like the playground's product box, plus the live text
# product data only comes back on a fresh crawl, so ask before the summary request caches the page
def bare(urls, timeout=LIVE_TIMEOUT):
    data = exa.request("/contents", {"urls": urls, "maxAgeHours": 0, "livecrawlTimeout": timeout})
    # cached copies have old prices
    fresh = {s["id"] for s in data.get("statuses", []) if s.get("source") == "crawled"}
    out = {}
    for r in data.get("results", []):
        listed = None
        entities = r.get("entities") or [] if r["id"] in fresh else []
        for e in entities:
            props = e.get("properties") or {}
            price = (props.get("offer") or {}).get("price") or {}
            if e.get("type") == "product" and price.get("amount"):
                listed = {"price": price["amount"], "currency": price.get("currency"), "name": props.get("name")}
        out[r["id"]] = {"text": r.get("text") or "", "listed": listed}
    return out

def listed_prices(urls, timeout=LIVE_TIMEOUT):
    return {u: p["listed"] for u, p in bare(urls, timeout).items() if p["listed"]}

# the summary often calls refurbished "new", so trust the link, the name and the shop's own data first
# only the link and name, because new product pages mention tweedekans as a side offer
REFURBISHED = re.compile(r"refurb|renewed|tweedekans|als.nieuw|zo.goed.als.nieuw|kunnostettu|generalüberholt", re.I)
USED = re.compile(r"\bused\b|second.?hand|tweedehands|gebruikt|käytetty|gebraucht|occasion", re.I)

def condition_of(page, info):
    named = page.url + " " + (info.get("product_name") or "")
    raw = page.text or ""
    if REFURBISHED.search(named):
        return "refurbished"
    if USED.search(named):
        return "used"
    if "schema.org/NewCondition" not in raw:
        if "schema.org/RefurbishedCondition" in raw:
            return "refurbished"
        if "schema.org/UsedCondition" in raw:
            return "used"
    return info.get("condition") if info.get("condition") in CONDITIONS else "new"

# price and doubt from the product data and the summary, checked against the visible text
def pick_price(listed, said, text):
    if listed and in_text(listed["price"], text):
        price, doubt = listed["price"], None
    # skip made up prices
    elif said and in_text(said, text):
        price, doubt = said, "disagree" if listed else None
    # js pages have no prices in their text, only the product data
    elif listed and not re.search(r"[€£]\s?\d|\b(EUR|GBP)\s?\d|\d,-|\d[.,]\d\d\s?[€£]", text):
        price, doubt = listed["price"], "unverified"
    else:
        return None
    return price, doubt

# doubt: None when the price is on the page, "unverified" when the page text shows no prices to check,
# "disagree" when the product data isn't on the page, "far_below_others" is set later by comparing shops
def to_offer(page, listed=None):
    info = json.loads(page.summary) if page.summary else {}
    text = visible(page.text or "")
    picked = pick_price(listed, info.get("price"), text)
    if not picked:
        return None
    info["price"], doubt = picked
    if listed:
        info["currency"] = listed["currency"] or info.get("currency")
        info["product_name"] = info.get("product_name") or listed["name"]
    was = info.get("was_price") or 0
    was = was if was > info["price"] and marked_was(was, info["price"], text) else None
    percent = round(info.get("discount_percent") or 0)
    if was:
        percent = round((was - info["price"]) / was * 100)
    elif not re.search(rf"(?<!\d){percent}\s?%", text):
        percent = 0
    when = page.crawl_date
    return {"url": page.url, "price": info["price"], "currency": info["currency"],
            "name": info.get("product_name"), "date": when[:10] if when else None,
            "claimed_price": was, "claimed_discount": percent or None, "doubt": doubt,
            "model": info.get("model") or "", "colour": info.get("colour") or "", "size": info.get("size") or "",
            "shop_page": bool(info.get("shop_page")) and not compares_shops(text, page.url),
            "condition": condition_of(page, info),
            "bundle": bool(info.get("bundle"))}

# local word for buy, gets local shops. the messenger sends its own, this is the fallback
BUY = {"NL": "kopen", "BE": "kopen", "FI": "osta", "DE": "kaufen", "GB": "buy"}

# only shop pages, no reviews or comparison sites
def search(query, country=None, domain=None, buy_word=None):
    if country:
        query = f"{query} {buy_word or BUY.get(country, 'buy')}"
    options = {"include_domains": [domain]} if domain else {}
    result = exa.search(query, num_results=10, type="auto", user_location=country, contents=CONTENTS, **options)
    return [offer for offer in map(to_offer, result.results) if offer and offer["shop_page"]]

def shop(url):
    return urlparse(url).netloc.removeprefix("www.")

# words with a digit in the query must be in the product, so "series s 512gb" doesn't pick a 1tb,
# and most other words too, so "zx 9000 flimflam" doesn't pick any zx 9000
def matches_query(query, offer):
    found = (offer["name"] or "") + " " + offer["model"] + " " + offer["size"]
    words = set(re.findall(r"[a-z0-9]+", found.lower()))
    packed = re.sub(r"[^a-z0-9]", "", found.lower())
    has = lambda k: k in words or (len(k) >= 3 and k in packed)
    query_words = re.findall(r"[a-z0-9]+", query.lower())
    keys = [w for w in query_words if re.search(r"\d", w)]
    names = [w for w in query_words if not re.search(r"\d", w) and len(w) >= 3]
    return all(map(has, keys)) and sum(map(has, names)) * 2 >= len(names)

def same_product(a, b):
    norm = lambda s: re.sub(r"[^A-Z0-9]", "", (s or "").upper())
    # a model without letters is a shop article number
    key = lambda o: norm(o["model"]) if re.search(r"[A-Z]", norm(o["model"])) else ""
    ka, kb = key(a), key(b)
    # WH1000XM6 matches WH1000XM6BCE7, not WH1000XM5
    fits = lambda k, m: len(m) >= 4 and (k.startswith(m) or m.startswith(k))
    named = lambda k, o: k and any(fits(k, norm(w)) for w in o["name"].split() if re.search(r"\d", w))
    if ka and kb:
        if not fits(ka, kb):
            return False
    elif not named(ka, b) and not named(kb, a):
        return False
    for field in ["colour", "size"]:
        x, y = norm(a[field]), norm(b[field])
        if x and y and "OTHER" not in (x, y) and x != y:
            return False
    return True

# far below the other shops is a missing vat, a wrong variant or a misread as often as a real deal
FAR_BELOW = 0.85

def flag_far_below(offers):
    for o in offers:
        others = sorted(x["price"] for x in offers if x is not o and x["currency"] == o["currency"])
        if len(others) >= 2 and o["price"] < others[len(others) // 2] * FAR_BELOW:
            o["doubt"] = "far_below_others"

# search results from other webshops
def cheaper_elsewhere(today, offers):
    cheaper = [o for o in offers
               if shop(o["url"]) != shop(today["url"])
               and o["currency"] == today["currency"]
               # a wrong low price would look like a deal
               and o["doubt"] in (None, "unverified", "far_below_others")
               # far cheaper is almost always a misread
               and today["price"] * 0.5 <= o["price"] < today["price"]]
    return sorted(cheaper, key=lambda o: o["price"])

def read(url, **options):
    result = exa.get_contents(url, **CONTENTS, **options)
    return to_offer(result.results[0]) if result.results else None

# search results are old copies, so check their price on the live page
# one bare request is enough, the rest comes from the search summary
def recheck(offers):
    if not offers:
        return []
    live = bare([o["url"] for o in offers], OTHERS_TIMEOUT)
    out = []
    for o in offers:
        page = live.get(o["url"])
        text = visible(page["text"]) if page else ""
        picked = page and pick_price(page["listed"], o["price"], text)
        if picked and not compares_shops(text, o["url"]):
            out.append({**o, "price": picked[0], "doubt": picked[1], "date": date.today().isoformat()})
    return out

# current product page
def page(url):
    listed = listed_prices([url])
    result = exa.get_contents(url, **CONTENTS, max_age_hours=0, livecrawl_timeout=LIVE_TIMEOUT)
    offer = to_offer(result.results[0], listed.get(result.results[0].id)) if result.results else None
    if offer:
        offer["date"] = offer["date"] or date.today().isoformat()
    return offer

def snapshot(url, as_of):
    offer = read(url, snapshot_as_of=as_of.isoformat())
    if offer:
        offer["date"] = as_of.date().isoformat()
    return offer

if __name__ == "__main__":
    import sys
    query = " ".join(sys.argv[1:])
    for offer in search(query, "NL"):
        print(offer["price"], offer["currency"], "|", offer["name"], "|", offer["url"])
