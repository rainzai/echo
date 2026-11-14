from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
import exa_client
from exa_client import cheaper_elsewhere, shop
from history import price_history, low_30d, is_fake_sale

app = FastAPI()
WORLDWIDE = 3

class CheckRequest(BaseModel):
    query: str
    country: str = Field("GB", pattern="^[A-Za-z]{2}$")

@app.post("/check")
def check(req: CheckRequest):
    country = "GB" if req.country.upper() == "UK" else req.country.upper()
    local = exa_client.search(req.query, country)
    world = [o for o in exa_client.search(req.query) if o["url"] not in {l["url"] for l in local}][:WORLDWIDE]
    offers = local + world
    # read live for todays price
    today = next(filter(None, (exa_client.page(o["url"]) for o in offers)), None)
    if not today:
        raise HTTPException(422, "Couldn't find a price for that")
    points = price_history(today["url"], today)
    low = low_30d(points, today)
    fake = is_fake_sale(today, low)
    others = cheaper_elsewhere(today, offers)[:3]

    sources = [{"n": i + 1, "url": p["url"], "date": p["date"], "label": f"{shop(p['url'])} on {p['date']}"}
               for i, p in enumerate(points)]
    elsewhere = []
    for o in others:
        sources.append({"n": len(sources) + 1, "url": o["url"], "date": o["date"], "label": shop(o["url"])})
        elsewhere.append({"url": o["url"], "shop": shop(o["url"]), "price": o["price"], "date": o["date"]})

    return {
        "query": req.query, "country": country,
        "url": today["url"], "name": today["name"], "currency": today["currency"],
        "price": today["price"],
        "low_30d": low, "fake_sale": fake,
        "history": points, "elsewhere": elsewhere,
        "offers": [{"url": o["url"], "shop": shop(o["url"]), "price": o["price"], "currency": o["currency"],
                    "local": o in local} for o in offers],
        "sources": sources,
    }
