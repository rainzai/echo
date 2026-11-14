import re
from concurrent.futures import ThreadPoolExecutor
from typing import Literal
from urllib.parse import urlparse, urlunparse, parse_qsl, urlencode
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import exa_client
from exa_client import cheaper_elsewhere, shop
from history import price_history, low_30d, is_fake_sale

app = FastAPI()
pool = ThreadPoolExecutor(8)
WORLDWIDE = 3
LIVE = 6
LINK = re.compile(r"https?://\S+")
TRACKING = re.compile(r"utm_.*|gclid|fbclid|msclkid|srsltid|igshid|mc_.*|cm_mmc.*|_ga|ref_?|tag|affiliate_id", re.I)

def clean_link(url):
    parts = urlparse(url.rstrip(".,!?)"))
    query = [(k, v) for k, v in parse_qsl(parts.query) if not TRACKING.fullmatch(k)]
    return urlunparse(parts._replace(query=urlencode(query), fragment=""))

# how far price is from other, in money and in percent of other
def diff(price, other):
    if other is None:
        return None, None
    return round(price - other, 2), round((price - other) / other * 100, 1)

class CheckRequest(BaseModel):
    query: str
    # where the user lives, not where the shop is; none searches everywhere
    country: str | None = Field(None, pattern="^[A-Za-z]{2}$")
    url: str | None = None
    # where the user saw it, other shops are still compared
    shop: str | None = None
    condition: Literal["new", "refurbished", "used"] = "new"
    # "buy" in the country's language, so exa finds local shops
    buy_word: str | None = None
    # local currency, so a kz search doesn't settle for russian shops
    currency: str | None = None

# bol, www.bol.com and https://bol.com/x all become bol.com
def domain(name):
    return shop(name if "//" in name else "//" + name).lower()

@app.post("/check")
def check(req: CheckRequest):
    country = req.country and ("GB" if req.country.upper() == "UK" else req.country.upper())
    link = req.url or ((m := LINK.search(req.query)) and m.group())
    today = None
    query = req.query
    fits = lambda o: (o["condition"] == req.condition and not o["bundle"] and exa_client.matches_query(req.query, o)
                      and (not req.currency or o["currency"] == req.currency.upper()))
    condition = req.condition
    if link:
        today = exa_client.page(clean_link(link))
        if not today:
            raise HTTPException(422, "Couldn't read a price on that link")
        # search other shops by the product name from the page
        query = today["name"]
        condition = today["condition"]
    # "refurbished" in the search, or exa only finds new ones
    if condition != "new":
        query = f"{query} {condition}"
    local_job = pool.submit(exa_client.search, query, country, None, req.buy_word, req.currency)
    world_job = pool.submit(exa_client.search, query) if country else None
    if not today and req.shop:
        seen = [o for o in exa_client.search(query, country, domain(req.shop), req.buy_word, req.currency) if fits(o)]
        today = next(filter(None, pool.map(exa_client.page, [o["url"] for o in seen[:2]])), None)
    local = local_job.result()
    world = [o for o in world_job.result() if o["url"] not in {l["url"] for l in local}][:WORLDWIDE] if world_job else []
    found = local + world
    local_urls = {o["url"] for o in local}
    if not today:
        # read live for todays price, a few at once so one slow shop doesn't hold it up
        today = next(filter(None, pool.map(exa_client.page, [o["url"] for o in found if fits(o)][:3])), None)
    if not today:
        raise HTTPException(422, "Couldn't find a price for that")
    shop_found = bool(req.shop) and shop(today["url"]).endswith(domain(req.shop))
    # refurbished is compared with refurbished, a bundle with bundles
    alike = lambda o: (o["url"] != today["url"] and exa_client.same_product(today, o)
                       and o["condition"] == today["condition"] and o["bundle"] == today["bundle"])
    history_job = pool.submit(price_history, today["url"], today)
    # search prices are old copies, so check them on the live pages
    offers = [today] + exa_client.recheck([o for o in found if alike(o)][:LIVE])
    exa_client.flag_far_below(offers)
    points = history_job.result()
    low = low_30d(points, today)
    fake = is_fake_sale(today, low)
    others = cheaper_elsewhere(today, offers)[:3]

    sources = [{"n": i + 1, "url": p["url"], "date": p["date"], "label": f"{shop(p['url'])} on {p['date']}"}
               for i, p in enumerate(points)]
    elsewhere = []
    for o in others:
        sources.append({"n": len(sources) + 1, "url": o["url"], "date": o["date"], "label": shop(o["url"])})
        elsewhere.append({"url": o["url"], "shop": shop(o["url"]), "price": o["price"], "date": o["date"],
                          "doubt": o["doubt"]})

    low_diff, low_pct = diff(today["price"], low)
    cheapest = elsewhere[0] if elsewhere else None
    cheapest_diff, cheapest_pct = diff(today["price"], cheapest and cheapest["price"])

    return {
        "query": req.query, "country": country,
        "url": today["url"], "name": today["name"], "currency": today["currency"],
        "price": today["price"], "price_doubt": today["doubt"],
        "condition": today["condition"], "bundle": today["bundle"], "shop_found": shop_found,
        "claimed_price": today["claimed_price"], "claimed_discount": today["claimed_discount"],
        "low_30d": low, "fake_sale": fake,
        "vs_low_30d": low_diff, "vs_low_30d_pct": low_pct,
        "cheapest_elsewhere": cheapest, "vs_cheapest": cheapest_diff, "vs_cheapest_pct": cheapest_pct,
        "history": points, "elsewhere": elsewhere,
        "offers": [{"url": o["url"], "shop": shop(o["url"]), "price": o["price"], "currency": o["currency"],
                    "local": o["url"] in local_urls} for o in offers],
        "sources": sources,
    }
