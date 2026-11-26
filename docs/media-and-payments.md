# Studio media and online top-ups

This guide is for self-hosted a1o-platform operators. The media channel and each payment method are optional. They use the existing application database and data volume.

## Volcengine Ark

1. In the admin console, add a **Volcengine Ark · Seedream / Seedance** channel. Enter your Ark API key, the exact model IDs your Ark account has enabled, and the user groups allowed to use them. This channel uses Ark’s Beijing API. To route through an Ark inference endpoint, keep the model ID in the channel's model list and map it to the endpoint ID.
2. Set `ARK_MEDIA_PRICES` to a JSON object keyed by the model names shown in Studio, then restart the app. The value is the base quota for one Seedream image or one requested Seedance video second. The user's group multiplier applies. Set prices for your deployment. These example values are site quota units and do not represent Volcengine's retail prices.

   ```dotenv
   ARK_MEDIA_PRICES='{"doubao-seedream-5-5-261028":10000,"doubao-seedance-2-5-260628":25000}'
   ```

3. Sign in to Studio with a user in an enabled group. In **Image generation**, select Seedream for one image with an optional reference, or Seedance for a 720p, 5–8 second video with an optional first frame. Both accept PNG, JPEG, and WebP reference images up to 5 MiB. Ark also applies model-specific input limits.
4. Run a small generation. Verify that the result is visible, downloads correctly, and draws the configured amount from your account quota. Seedance status checks resume after a server restart for tasks with a saved upstream ID. The app streams completed Seedance videos to disk with a 64 MiB per-file limit. Back up the database together with its media files.

The worker reserves quota before generation. Confirmed upstream failures release the reservation. If the initial request was interrupted before Ark confirmed its outcome, the task requires reconciliation. Compare its record and the original channel with the Ark console before adjusting quota. A saved Seedance task ID keeps the worker on the original channel for subsequent checks. Keep that channel and its credentials available until the tasks have settled.

Image workers renew a 90-second lease while executing a job. Expired jobs with a saved result can retry saving. Jobs with an unknown upstream outcome go to reconciliation. The first upgrade to this lease format adds a database column. In a shared-database deployment, let active image jobs drain, stop the old instances, and start the upgraded primary so it applies the schema update before starting the other instances.

[Ark image API](https://docs.volcengine.com/docs/ark/image-generation-api?lang=en) · [Ark video tutorial](https://docs.volcengine.com/docs/ark/video-generation-tutorial?lang=zh)

## Online quota packages

1. Set `PAYMENT_PUBLIC_URL` to the public HTTPS origin of Studio, without a path, query string, or fragment.
2. Set `PAYMENT_PACKAGES` to a JSON array with up to 24 fixed-price packages. Give each package a unique ID, label, at least one price, and quota in site units. Set `amount` in USD cents for Stripe and NOWPayments, and `cny_amount` in Chinese fen for EPay. Enter both prices to offer the same package through all enabled gateways, or omit a price to hide those gateways for that package. The configurable minimums are USD 0.50 and CNY 0.01. Gateways can require higher minimums. This example sells a one-time grant of 7,500,000 site quota units for USD 15 or CNY 99.

   ```dotenv
   PAYMENT_PUBLIC_URL=https://studio.example.com
   PAYMENT_PACKAGES='[{"id":"standard","name":"Standard","amount":1500,"cny_amount":9900,"quota":7500000}]'
   ```

3. Configure the gateways you want to offer, then start the app with the new environment. With Docker Compose, set the variables in the project's environment or Compose `.env` file. Run `docker compose up -d a1o-platform` for the bundled PostgreSQL deployment, or add the same `-f` option you selected in the [database deployment guide](database-deployment.md) before `up`. For a direct container run, pass the variables when creating the container and retain the existing data mount when replacing it. Use Stripe test credentials on a separate test deployment or database. The NOWPayments adapter uses the production API.

   | Gateway | Setup |
   | --- | --- |
   | Stripe | Set `STRIPE_SECRET_KEY` and `STRIPE_WEBHOOK_SECRET`. Register `POST /api/payments/webhook/stripe` at the public origin for `checkout.session.completed`, `checkout.session.async_payment_succeeded`, and `checkout.session.expired`. |
   | NOWPayments | Set `NOWPAYMENTS_API_KEY`, `NOWPAYMENTS_IPN_SECRET`, and `NOWPAYMENTS_PAY_CURRENCIES`. Enable the same payment currencies in the NOWPayments merchant dashboard and check its minimum for each enabled currency and package. Invoices send notifications to `POST /api/payments/webhook/nowpayments` at the public origin. |
   | EPay (易支付) | Use a 彩虹易支付 V1-compatible gateway. Set `EPAY_GATEWAY_URL` to its HTTPS base URL without `/submit.php`, query, or fragment. Set `EPAY_MERCHANT_ID` and `EPAY_MERCHANT_KEY` from the merchant console. The hosted cashier lets users choose among the channels enabled by your gateway. If your gateway requires a channel or you want to use a single channel, set `EPAY_PAYMENT_TYPE` to its code, such as `alipay` or `wxpay`. |

   Set `NOWPAYMENTS_PAY_CURRENCIES` to one code or a comma-separated list. Studio shows the enabled options in this order and selects the first one by default. For example, the setting below enables USDT on Solana, USDC on Polygon, and BTC on Bitcoin.

   ```dotenv
   NOWPAYMENTS_PAY_CURRENCIES=usdtsol,usdcmatic,btc
   ```

   | Asset | Network and configuration code |
   | --- | --- |
   | USDT | TRON `usdttrc20`, Ethereum `usdterc20`, BNB Smart Chain `usdtbsc`, Solana `usdtsol`, Polygon `usdtmatic` |
   | USDC | Ethereum `usdc`, Solana `usdcsol`, Polygon `usdcmatic` |
   | BTC | Bitcoin `btc` |
   | ETH | Ethereum `eth`, Arbitrum `etharb` |
   | SOL | Solana `sol` |

   An unset or empty list disables new NOWPayments checkouts. An unsupported code also disables new NOWPayments checkouts and is reported in the server log when the payment catalog is requested. To keep offering USDT on TRON after upgrading from the single-network version, include `usdttrc20`. Keep the merchant API key and IPN secret configured until open orders settle, even after disabling their payment currency for new orders.

4. Open **Account → Redeem** in Studio. Verify the packages, gateways, prices, and enabled currencies. Use a Stripe test payment to check that the order shows as credited and the account gains the configured quota. Replay its notification and verify that the balance stays the same. Repeat those checks in a controlled merchant checkout for each enabled NOWPayments option and EPay channel before offering them to users.

EPay sends signed notifications to `/api/payments/webhook/epay` at the public origin. Allow unauthenticated GET requests to that path through your proxy. The app also accepts URL-encoded POST bodies at the same path. Its app logs omit the EPay notification and return query strings. Configure your reverse proxy to omit them for `/api/payments/webhook/epay` and `/api/payments/return/epay` as well. To stop offering new EPay checkouts, unset `EPAY_GATEWAY_URL`. Keep the same merchant ID and key configured until outstanding notifications have settled.

Each order keeps its original price and quota even if you edit the catalog. NOWPayments orders also keep their original payment currency and network. Orders created before the currency setting was introduced are still verified as USDT on TRON. The gateway's verified notification triggers credit. A successful browser redirect can arrive before the notification. Underpaid or mismatched orders require manual review against the provider's records. Handle refunds and chargebacks in the provider dashboard and reconcile site quota manually. Existing redemption codes and administrator top-ups continue to work. Online packages grant quota once and have no renewal or periodic quota reset.

[Stripe Checkout](https://docs.stripe.com/checkout/fulfillment) · [NOWPayments invoice API](https://nowpayments.zendesk.com/hc/en-us/articles/21345824322717-API-and-endpoint-description) · [NOWPayments supported currencies](https://nowpayments.io/supported-coins) · [EPay V1 API reference](https://pay.myzfw.com/doc/v1_legacy_api.html)
