import Deck from "../src/deck";
import { orderedCards } from "./fixtures";

describe("Deck", () => {
  const deck = new Deck();
  deck.order();

  test(".order", () => {
    expect(deck.cards).toEqual(orderedCards);
  });
});
