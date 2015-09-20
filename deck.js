function Card(suitName, value, image){
    this.suit = suitName;
    this.value = value;
    this.image = image;
}
Card.prototype.toString = function(){
    return this.value + " of " + this.suit;
};

function shuffle(array) {
  var currentIndex = array.length,
      temporaryValue,
      randomIndex ;

    // While there remain elements to shuffle...
    while (0 !== currentIndex) {

        // Pick a remaining element...
        randomIndex = Math.floor(Math.random() * currentIndex);
        currentIndex -= 1;

        // And swap it with the current element.
        temporaryValue = array[currentIndex];
        array[currentIndex] = array[randomIndex];
        array[randomIndex] = temporaryValue;
    }
    return array;
}

function initDeck(){
    var deck = [];
    var suits = ["hearts", "diamonds", "spades", "clubs"];
    var edgeCards = {
        1: "ace",
        11: "jack",
        12: "queen",
        13: "king"
    };
    suits.forEach(function(suit){
        for (var i = 0; i < 13; i++){
            var cardValue = edgeCards[i+1] || i+1;
            var pathToImg = "img/" + i+1 + "_of_" + suit +".svg";
            deck.push(new Card(suit, i+1, "img/" + pathToImg));
        }
    });
    return shuffle(deck);
}
var deck = initDeck();
console.log(deck);