<h1 align="center">Echo</h1>
<p align="center">
  Text it a deal on iMessage and it tells you if the discount is real
  <br />
  <img src="https://img.shields.io/badge/iMessage-ask.echo%40icloud.com-34C759?logo=apple&logoColor=white" alt="iMessage ask.echo@icloud.com">
</p>

## Try it

Text **ask.echo@icloud.com** on iMessage. For example:

- a product link: `https://www.amazon.nl/dp/B0CJFT5BZ3 is this a good deal?`
- a name and a price: `ps5 pro for 699 at mediamarkt, good?`
- a screenshot of a shop page or ad, or a photo of a price tag
- `sources` after a check, to see every price with its date and link

An answer takes about fifteen seconds.

## About

Shops raise prices before a sale so the "-40%" looks good, or show a "was" price the product never sold for. In the EU a discount has to be compared to the lowest price of the last 30 days, but nobody checks that while scrolling an ad.

Echo reads the same product page as it was up to 90 days ago with Exa Snapshot, so the old prices come from the shop itself and not from a model. Gemini 4 Hydrogen runs the conversation, NVIDIA Nemotron 3.5 Super VL reads photos and screenshots, and the price comparisons are plain code.

## How it works

The steps run in order for every message.

- Messages (BlueBubbles)
    - Receives iMessages on a Mac and sends the replies, 👍 and typing bubble
- Photos (Nemotron 3.5 Super VL)
    - Reads product, model, price, claimed old price and shop from the image
    - Says when it isn't sure or sees more than one product
- Conversation (Gemini 4 Hydrogen)
    - Decides when to check a price, asks when something is missing, remembers the chat
    - Gemini 3.8 Flash takes over a step when Hydrogen returns an empty answer
- Check (Exa)
    - Search finds the product in shops in the user's country, with the local word for "buy" and the currency
    - Contents reads the live price and the claimed discount as structured data
    - Snapshot reads the same page as it was 7 to 90 days ago
- Verdict (code, then Gemini)
    - Code works out the 30-day low, whether the discount is real and which shops are cheaper
    - Gemini puts that into a short reply in the user's language

## Repo

- `messenger/`: Go. iMessage via BlueBubbles, the Gemini agent, Nemotron, evals
- `dealcheck/`: Python. Exa search, live contents and Snapshot price history

## Privacy

Echo keeps each chat in memory only, and forgets it on restart. Like any iMessage, the messages and photos also live in Messages on the Mac that runs Echo, and the log shows message text. To answer, message text goes to Gemini, photos to Nemotron and product names to Exa.

## Evals

- `messenger/eval/verdict`: 36 conversations (fake and real discounts, doubtful prices, other currencies, photos, people trying to trick it), checked by code and judged by a second Gemini model. 179/180 on the final prompt.
- `messenger/eval`: 28 photos and screenshots with the right answer for Nemotron. Confidently wrong readings went from 12 to 5 on the same 21 photos.

## Development

```
# env
cp .env.example .env # fill in BlueBubbles, Exa, Gemini and NVIDIA keys

# dealcheck (python + uv)
cd dealcheck && uv run uvicorn app:app --port 8000

# messenger (go), needs BlueBubbles running with the Private API
cd messenger && go run .

# evals
cd messenger && go test -tags eval ./agent -run Verdict -v -runs 5
python3 messenger/eval/score.py 5
```
