package mahjong2

import (
	"errors"
	"sort"
	"time"
)

func (r *Round) Draw(seat int, t time.Time) (drawn Tile, flowers []Tile, err error) {
	if r.Turn != seat {
		err = errors.New("wrong turn")
		return
	}
	if r.Phase != PhaseDraw {
		err = errors.New("wrong phase")
		return
	}
	if t.Before(r.LastDiscardTime.Add(r.ReservedDuration)) {
		err = errors.New("cannot draw during reserved duration")
		return
	}
	drawn = r.drawFront()
	flowers = make([]Tile, 0)
	for isFlower(drawn) {
		flowers = append(flowers, drawn)
		drawn = r.drawBack()
	}
	hand := &r.Hands[seat]
	hand.Concealed.Add(drawn)
	hand.Flowers = append(hand.Flowers, flowers...)
	r.Phase = PhaseDiscard
	return
}

func (r *Round) Discard(seat int, t time.Time, tile Tile) error {
	if seat != r.Turn {
		return errors.New("wrong turn")
	}
	if r.Phase != PhaseDiscard {
		return errors.New("wrong phase")
	}
	if !r.Hands[seat].Concealed.Contains(tile) {
		return errors.New("missing tiles")
	}
	r.Hands[seat].Concealed.Remove(tile)
	r.Discards = append(r.Discards, tile)
	r.Turn = (r.Turn + 1) % 4
	r.Phase = PhaseDraw
	return nil
}

func (r *Round) Chi(seat int, t time.Time, tile1, tile2 Tile) error {
	if r.Turn != seat {
		return errors.New("wrong turn")
	}
	if r.Phase != PhaseDraw {
		return errors.New("wrong phase")
	}
	if len(r.Discards) == 0 {
		return errors.New("no discards")
	}
	others, ok := sequences[r.lastDiscard()]
	if !ok {
		return errors.New("cannot chi non-suited tile")
	}
	valid := false
	for _, tiles := range others {
		if (tile1 == tiles[0] && tile2 == tiles[1]) || (tile1 == tiles[1] && tile2 == tiles[0]) {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("invalid sequence")
	}
	hand := &r.Hands[seat]
	if !hand.Concealed.Contains(tile1) || !hand.Concealed.Contains(tile2) {
		return errors.New("missing tiles")
	}
	if t.Before(r.LastDiscardTime.Add(r.ReservedDuration)) {
		return errors.New("cannot chi during reserved duration")
	}
	hand.Concealed.Remove(tile1)
	hand.Concealed.Remove(tile2)
	tile0 := r.popLastDiscard()
	seq := []Tile{tile0, tile1, tile2}
	sort.Slice(seq, func(i, j int) bool {
		return seq[i] < seq[j]
	})
	hand.Revealed = append(hand.Revealed, Meld{
		Type:  MeldChi,
		Tiles: seq,
	})
	r.Phase = PhaseDiscard
	return nil
}

func (r *Round) Pong(seat int, t time.Time) error {
	if seat == r.previousTurn() {
		return errors.New("wrong turn")
	}
	if r.Phase != PhaseDraw {
		return errors.New("wrong phase")
	}
	if len(r.Discards) == 0 {
		return errors.New("no discards")
	}
	hand := &r.Hands[seat]
	if hand.Concealed.Count(r.lastDiscard()) < 2 {
		return errors.New("missing tiles")
	}
	tile := r.popLastDiscard()
	hand.Concealed.RemoveN(tile, 2)
	hand.Revealed = append(hand.Revealed, Meld{
		Type:  MeldPong,
		Tiles: []Tile{tile},
	})
	r.Turn = seat
	r.Phase = PhaseDiscard
	return nil
}

func (r *Round) GangFromDiscard(seat int, t time.Time) (replacement Tile, flowers []Tile, err error) {
	if seat == r.previousTurn() {
		err = errors.New("wrong turn")
		return
	}
	if r.Phase != PhaseDraw {
		err = errors.New("wrong phase")
		return
	}
	if len(r.Discards) == 0 {
		err = errors.New("no discards")
		return
	}
	hand := &r.Hands[seat]
	if hand.Concealed.Count(r.lastDiscard()) < 3 {
		err = errors.New("missing tiles")
		return
	}
	tile := r.popLastDiscard()
	hand.Concealed.RemoveN(tile, 3)
	hand.Revealed = append(hand.Revealed, Meld{
		Type:  MeldGang,
		Tiles: []Tile{tile},
	})
	replacement, flowers = r.replaceTile()
	hand.Flowers = append(hand.Flowers, flowers...)
	hand.Concealed.Add(replacement)
	r.Turn = seat
	r.Phase = PhaseDiscard
	return
}

func (r *Round) GangFromHand(seat int, t time.Time, tile Tile) (replacement Tile, flowers []Tile, err error) {
	if seat != r.Turn {
		err = errors.New("wrong turn")
		return
	}
	if r.Phase != PhaseDiscard {
		err = errors.New("wrong phase")
		return
	}
	hand := &r.Hands[seat]
	if hand.Concealed.Count(tile) == 4 {
		hand.Concealed.RemoveN(tile, 4)
		hand.Revealed = append(hand.Revealed, Meld{
			Type:  MeldGang,
			Tiles: []Tile{tile},
		})
		replacement, flowers = r.replaceTile()
		hand.Flowers = append(hand.Flowers, flowers...)
		hand.Concealed.Add(replacement)
		return
	}
	for i, meld := range hand.Revealed {
		if meld.Type == MeldPong && meld.Tiles[0] == tile && hand.Concealed.Count(tile) > 0 {
			hand.Concealed.Remove(tile)
			hand.Revealed[i].Type = MeldGang
			replacement, flowers = r.replaceTile()
			hand.Flowers = append(hand.Flowers, flowers...)
			hand.Concealed.Add(replacement)
			return
		}
	}
	err = errors.New("missing tiles")
	return
}
