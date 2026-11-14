import sys
from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime, timedelta, timezone
import exa_client

DAYS_BACK = [7, 14, 21, 30, 45, 60, 90]

def try_snapshot(url, as_of):
    try:
        return exa_client.snapshot(url, as_of)
    except ValueError as err:  # for when snapshot runs out
        print("snapshot failed:", err, file=sys.stderr)
        return None

def price_history(url, today):
    now = datetime.now(timezone.utc)
    with ThreadPoolExecutor() as pool:
        snaps = pool.map(lambda days: try_snapshot(url, now - timedelta(days=days)), DAYS_BACK)
    points = {s["date"]: s for s in snaps if s and s["date"]}
    points[today["date"]] = today
    return [{"date": p["date"], "price": p["price"], "url": p["url"]}
            for p in sorted(points.values(), key=lambda p: p["date"])]

# lowest price in the last 30 days
def low_30d(points, today):
    since = (date.fromisoformat(today["date"]) - timedelta(days=30)).isoformat()
    prices = [p["price"] for p in points if since <= p["date"] < today["date"]]
    return min(prices) if prices else None

def is_fake_sale(today, low):
    return low is not None and low <= today["price"]

if __name__ == "__main__":
    # testable
    import sys
    url = sys.argv[1]
    now = datetime.now(timezone.utc)
    for days in [0, 30, 90]:
        offer = exa_client.page(url) if days == 0 else try_snapshot(url, now - timedelta(days=days))
        print(f"-{days}d:", offer and (offer["date"], offer["price"], offer["currency"]))
