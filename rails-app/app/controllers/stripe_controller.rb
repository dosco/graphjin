class StripeController < ApplicationController
  # GET /stripe/1
  # GET /stripe/1.json
  def show
    data = '{ "data": [
      {
        "id": 1,
        "customer_id": "$id",
        "object": "charge",
        "amount": 100,
        "amount_refunded": 0,
        "date": "01/01/2019",
        "application": null,
        "billing_details": {
          "address": "1 Infinity Drive",
          "zipcode": "94024"
        }
      },
      {
        "id": 2,
        "customer_id": "$id",
        "object": "charge",
        "amount": 150,
        "amount_refunded": 0,
        "date": "02/18/2019",
        "billing_details": {
          "address": "1 Infinity Drive",
          "zipcode": "94024"
        }
      },
      {
        "id": 3,
        "customer_id": "$id",
        "object": "charge",
        "amount": 150,
        "amount_refunded": 50,
        "date": "03/21/2019",
        "billing_details": {
          "address": "1 Infinity Drive",
          "zipcode": "94024"
        }
      }
    ],
    "data_type": "charges",
    "total_count": 3,
    "next_cursor": null
    }'

    data.gsub!("$id", params[:id])
    result = JSON.parse(data)

    render json: result

  end
end
