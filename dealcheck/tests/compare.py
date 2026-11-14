# run: uv run python tests/compare.py [exa labelled html own]
import csv
import html
import json
import re
import sys

import requests

sys.path.insert(0, ".")
import exa_client

BROWSER = {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "nl,en;q=0.8,fi;q=0.6",
}

LABELLED = {
    "type": "object",
    "properties": {
        "product_name": {"type": "string"},
        "currency": {"type": "string", "description": "ISO currency code, e.g. EUR"},
        "prices": {
            "type": "array",
            "description": "Every price the page shows for this exact product, in the order they appear. "
                           "Leave out prices of other products, accessories and bundles.",
            "items": {
                "type": "object",
                "properties": {
                    "amount": {"type": "number"},
                    "kind": {"type": "string", "enum": ["current", "original", "refurbished", "member", "monthly", "other"],
                             "description": "current: what anyone pays right now for a new one. "
                                            "original: crossed-out, list, advice or 'was' price. "
                                            "refurbished: second-hand, returned or outlet. "
                                            "member: needs a membership, code or trade-in. "
                                            "monthly: an instalment."},
                },
                "required": ["amount", "kind"],
            },
        },
    },
    "required": ["product_name", "currency", "prices"],
}

def to_number(value):
    amount = re.sub(r"[^\d.,]", "", str(value))
    whole, cents = re.fullmatch(r"(.*?)(?:[.,](\d{1,2}))?", amount).groups()
    whole = re.sub(r"[.,]", "", whole)
    return float(f"{whole or 0}.{cents or 0}") if whole or cents else None

def offers(node):
    if isinstance(node, list):
        for item in node:
            yield from offers(item)
    elif isinstance(node, dict):
        types = node.get("@type")
        if "Product" in (types if isinstance(types, list) else [types]):
            found = node.get("offers") or []
            yield from (o for o in (found if isinstance(found, list) else [found]) if isinstance(o, dict))
        else:
            for value in node.values():
                yield from offers(value)

def meta(page, name):
    tag = re.search(rf'<meta[^>]*(?:product|og):price:{name}["\'][^>]*>', page)
    content = tag and re.search(r'content=["\']([^"\']+)', tag.group(0))
    return content and content.group(1)

def own_price(page):
    for block in re.findall(r"<script[^>]*application/ld\+json[^>]*>(.*?)</script>", page, re.S | re.I):
        try:
            data = json.loads(html.unescape(block) if "&quot;" in block else block)
        except ValueError:
            continue
        for offer in offers(data):
            price = to_number(offer.get("price") or offer.get("lowPrice") or "")
            if price:
                return price, offer.get("priceCurrency")
    price = to_number(meta(page, "amount") or "")
    return (price, meta(page, "currency")) if price else None

def exa(url):
    offer = exa_client.page(url)
    return offer and (offer["price"], offer["currency"])

def labelled(url):
    result = exa_client.exa.get_contents(
        url, text=True, max_age_hours=0,
        summary={"query": "List every price this page shows for this product.", "schema": LABELLED})
    page = result.results[0] if result.results else None
    info = json.loads(page.summary) if page and page.summary else {}
    for price in info.get("prices") or []:
        if price.get("kind") == "current" and price.get("amount") and exa_client.in_text(price["amount"], page.text or ""):
            return price["amount"], info.get("currency")
    return None

def html_tags(url):
    result = exa_client.exa.get_contents(url, text={"include_html_tags": True}, max_age_hours=0)
    return own_price(result.results[0].text or "") if result.results else None

def own(url):
    try:
        response = requests.get(url, headers=BROWSER, timeout=25)
    except requests.RequestException:
        return None
    return own_price(response.text) if response.ok else None

READERS = {"exa": exa, "labelled": labelled, "html": html_tags, "own": own}

if __name__ == "__main__":
    names = sys.argv[1:] or ["exa", "labelled", "html"]
    rows = [r for r in csv.DictReader(open("tests/golden.csv")) if r["url"]]
    scored = sum(1 for r in rows if r["expected_price"])
    right = dict.fromkeys(names, 0)
    wrong = dict.fromkeys(names, 0)
    for r in rows:
        want = float(r["expected_price"]) if r["expected_price"] else None
        line = f"{r['country']} {r['shop']:<17} want {want if want is not None else '?':>7} {r['currency']}"
        for name in names:
            try:
                got = READERS[name](r["url"])
            except ValueError as err:
                print(f"{name} failed on {r['shop']}: {err}", file=sys.stderr)
                got = None
            if got is None:
                mark = "NO PRICE"
            elif want is None:
                mark = ""
            elif abs(got[0] - want) < 0.01 and got[1] == r["currency"]:
                mark, right[name] = "OK", right[name] + 1
            else:
                mark, wrong[name] = "WRONG", wrong[name] + 1
            line += f" | {name} {got[0] if got else '':>7} {mark:<8}"
        print(line)
    print()
    for name in names:
        print(f"{name}: {right[name]}/{scored} correct, {wrong[name]} wrong, {scored - right[name] - wrong[name]} no price")
