module.exports = function () {
  var faker = require("faker");
  var _ = require("lodash");
  return {
    payments: _.times(100, function (n) {
      return {
        id: "ch_" + n,
        object: faker.finance.transactionType(),
        amount: faker.finance.account(),
        balance_transaction: "txn_" + faker.random.alphaNumeric(),
        billing_details: {
          email: faker.internet.email(),
          phone: faker.phone.phoneNumber(),
          address: faker.address.streetAddress(),
        },
      };
    }),
    /*
    categories: _.times(6, function (n) {
      return {
        image: faker.random.image(),
        short_description: faker.lorem.sentence(),
        category_ID: faker.random.uuid(),
      };
    }),

    sub_categories: _.times(100, function (n) {
      return {
        image: faker.random.image(),
        short_description: faker.lorem.sentence(),
        category_ID: faker.random.uuid(),
      };
    }),

    search: _.times(100, function (n) {
      return {
        image: faker.random.image(),
        short_description: faker.lorem.sentence(),
        gtin13: faker.random.uuid(),
        price: faker.commerce.price(),
      };
    }),
    //, ...
    */
  };
};
