import json
import re
from datetime import date
from urllib.parse import urlparse
from dotenv import load_dotenv
from exa_py import Exa

load_dotenv()
exa = Exa()

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
    },
    "required": ["product_name", "price", "currency"],
}
PRICE_QUESTION = "What is this product's selling price right now?"
CONTENTS = {"text": True, "summary": {"query": PRICE_QUESTION, "schema": SCHEMA}}

# matches 1.299,00 and 1,299.00 and 319,-
def in_text(price, text):
    whole, cents = divmod(round(price * 100), 100)
    groups = re.findall(r"\d{1,3}(?=(?:\d{3})*$)", str(whole))
    pattern = r"(?<!\d)" + r"[.,  ]?".join(groups) + (rf"\D{{0,3}}{cents:02d}" if cents else "") + r"(?!\d)"
    return re.search(pattern, text) is not None

def to_offer(page):
    if not page.summary:
        return None
    info = json.loads(page.summary)
    # skip made up prices
    if not info.get("price") or not in_text(info["price"], page.text or ""):
        return None
    when = page.crawl_date
    return {"url": page.url, "price": info["price"], "currency": info["currency"],
            "name": info.get("product_name"), "date": when[:10] if when else None}

# local word for buy, gets local shops
BUY = {"NL": "kopen", "BE": "kopen", "FI": "osta", "DE": "kaufen", "GB": "buy"}

def search(query, country=None):
    if country:
        query = f"{query} {BUY.get(country, 'buy')}"
    result = exa.search(query, num_results=10, type="auto", user_location=country, contents=CONTENTS)
    return [offer for offer in map(to_offer, result.results) if offer]

def shop(url):
    return urlparse(url).netloc.removeprefix("www.")

# search results from other webshops
def cheaper_elsewhere(today, offers):
    cheaper = [o for o in offers
               if shop(o["url"]) != shop(today["url"])
               and o["currency"] == today["currency"]
               and o["price"] < today["price"]]
    return sorted(cheaper, key=lambda o: o["price"])

def read(url, **options):
    result = exa.get_contents(url, **CONTENTS, **options)
    return to_offer(result.results[0]) if result.results else None

# current product page
def page(url):
    offer = read(url, max_age_hours=0)
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
