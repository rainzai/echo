# run from dealcheck: uv run python tests/compare.py
import csv
import sys

sys.path.insert(0, ".")
import exa_client

if __name__ == "__main__":
    rows = [r for r in csv.DictReader(open("tests/golden.csv")) if r["url"] and r["expected_price"]]
    right = wrong = 0
    for r in rows:
        want = float(r["expected_price"])
        try:
            got = exa_client.page(r["url"])
        except ValueError as err:
            print("exa failed:", err, file=sys.stderr)
            got = None
        if got is None:
            mark = "NO PRICE"
        elif abs(got["price"] - want) < 0.01 and got["currency"] == r["currency"]:
            mark, right = "OK", right + 1
        else:
            mark, wrong = "WRONG", wrong + 1
        print(f"{r['country']} {r['shop']:<17} want {want:>7} {r['currency']} | got {got['price'] if got else '':>7} {mark}")
    print(f"\n{right}/{len(rows)} correct, {wrong} wrong, {len(rows) - right - wrong} no price")
