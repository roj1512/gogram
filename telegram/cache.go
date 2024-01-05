// Copyright (c) 2024 RoseLoverX

package telegram

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/roj1512/gogram/internal/utils"
)

const (
	// CacheUpdateInterval is the interval in seconds at which the cache is updated
	CacheUpdateInterval = 60
)

type CACHE struct {
	*sync.RWMutex
	chats      map[int64]*ChatObj
	users      map[int64]*UserObj
	channels   map[int64]*Channel
	InputPeers *InputPeerCache `json:"input_peers,omitempty"`
	logger     *utils.Logger
}

func (cache *CACHE) Pin(pinner *runtime.Pinner) {
	pinner.Pin(cache)
	pinner.Pin(cache.RWMutex)
	pinner.Pin(cache.logger)
	pinner.Pin(cache.InputPeers)
}

type InputPeerCache struct {
	InputChannels map[int64]int64 `json:"channels,omitempty"`
	InputUsers    map[int64]int64 `json:"users,omitempty"`
	InputChats    map[int64]int64 `json:"chats,omitempty"`
}

func (c *CACHE) flushToFile() {
	c.Lock()
	defer c.Unlock()

	data, err := json.Marshal(c)
	if err != nil {
		c.logger.Error("Error while marshalling cache: ", err)
		return
	}

	file, err := os.Create("cache.journal")
	if err != nil {
		c.logger.Error("Error while creating cache.journal: ", err)
		return
	}

	if _, err := io.WriteString(file, string(data)); err != nil {
		c.logger.Error("Error while writing cache.journal: ", err)
		return
	}
	if err := file.Close(); err != nil {
		c.logger.Error("Error while closing cache.journal: ", err)
		return
	}
	go time.AfterFunc(80*time.Second, c.flushToFile)
}

func (c *CACHE) loadFromFile() {
	file, err := os.Open("cache.journal")
	if err != nil {
		if os.IsNotExist(err) {
			// cache file doesn't exist, this is not an error
			return
		}
		c.logger.Error("Error while opening cache.journal: ", err)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		c.logger.Error("Error while getting cache.journal file info: ", err)
		return
	}
	if stat.Size() == 0 {
		// empty cache file, nothing to load
		return
	}

	data := make([]byte, stat.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		c.logger.Error("Error while reading cache.journal: ", err)
		return
	}

	if err := json.Unmarshal(data, c); err != nil {
		c.logger.Error("Error while unmarshalling cache.journal: ", err)
		return
	}
}

func (c *CACHE) ExportJSON() ([]byte, error) {
	c.RLock()
	defer c.RUnlock()

	return json.Marshal(c.InputPeers)
}

func (c *CACHE) ImportJSON(data []byte) error {
	c.Lock()
	defer c.Unlock()

	return json.Unmarshal(data, c.InputPeers)
}

var cache = NewCache()

func NewCache() *CACHE {
	c := &CACHE{
		RWMutex:  &sync.RWMutex{},
		chats:    make(map[int64]*ChatObj),
		users:    make(map[int64]*UserObj),
		channels: make(map[int64]*Channel),
		InputPeers: &InputPeerCache{
			InputChannels: make(map[int64]int64),
			InputUsers:    make(map[int64]int64),
			InputChats:    make(map[int64]int64),
		},
		logger: utils.NewLogger("cache").SetLevel(LIB_LOG_LEVEL),
	}
	c.logger.Debug("Cache initialized successfully")

	return c
}

func (c *CACHE) startCacheFileUpdater() {
	c.loadFromFile()
	go c.writeOnKill()
	go time.AfterFunc(80*time.Second, c.flushToFile)
}

func (c *CACHE) writeOnKill() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	sig := <-signals
	c.logger.Debug("\nReceived signal: " + sig.String() + ", flushing cache to file and exiting...\n")
	c.flushToFile()
}

func (c *CACHE) getUserPeer(userID int64) (InputUser, error) {
	for userId, accessHash := range c.InputPeers.InputUsers {
		if userId == userID {
			return &InputUserObj{UserID: userId, AccessHash: accessHash}, nil
		}
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *CACHE) getChannelPeer(channelID int64) (InputChannel, error) {
	for channelId, channelHash := range c.InputPeers.InputChannels {
		if channelId == channelID {
			return &InputChannelObj{ChannelID: channelId, AccessHash: channelHash}, nil
		}
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}

func (c *CACHE) GetInputPeer(peerID int64) (InputPeer, error) {
	// if peerID is negative, it is a channel or a chat
	if strings.HasPrefix(strconv.Itoa(int(peerID)), "-100") {
		// remove -100 from peerID
		peerIdStr := strconv.Itoa(int(peerID))
		peerIdStr = strings.TrimPrefix(peerIdStr, "-100")
		peerIdInt, err := strconv.Atoi(peerIdStr)
		if err != nil {
			return nil, err
		}
		peerID = int64(peerIdInt)
	}
	c.RLock()
	defer c.RUnlock()
	for userId, userHash := range c.InputPeers.InputUsers {
		if userId == peerID {
			return &InputPeerUser{userId, userHash}, nil
		}
	}
	for chatId := range c.InputPeers.InputChats {
		if chatId == peerID {
			return &InputPeerChat{ChatID: chatId}, nil
		}
	}
	for channelId, channelHash := range c.InputPeers.InputChannels {
		if channelId == peerID {
			return &InputPeerChannel{channelId, channelHash}, nil
		}
	}
	return nil, fmt.Errorf("there is no peer with id %d or missing from cache", peerID)
}

// ------------------ Get Chat/Channel/User From Cache/Telgram ------------------

func (c *Client) getUserFromCache(userID int64) (*UserObj, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()
	for _, user := range c.Cache.users {
		if user.ID == userID {
			return user, nil
		}
	}
	userPeer, err := c.Cache.getUserPeer(userID)
	if err != nil {
		return nil, err
	}
	users, err := c.UsersGetUsers([]InputUser{userPeer})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("no user with id %d", userID)
	}
	user, ok := users[0].(*UserObj)
	if !ok {
		return nil, fmt.Errorf("no user with id %d", userID)
	}
	return user, nil
}

func (c *Client) getChannelFromCache(channelID int64) (*Channel, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()

	for _, channel := range c.Cache.channels {
		if channel.ID == channelID {
			return channel, nil
		}
	}
	channelPeer, err := c.Cache.getChannelPeer(channelID)
	if err != nil {
		return nil, err
	}
	channels, err := c.ChannelsGetChannels([]InputChannel{channelPeer})
	if err != nil {
		return nil, err
	}
	channelsObj, ok := channels.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	if len(channelsObj.Chats) == 0 {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	channel, ok := channelsObj.Chats[0].(*Channel)
	if !ok {
		return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
	}
	return channel, nil
}

func (c *Client) getChatFromCache(chatID int64) (*ChatObj, error) {
	c.Cache.RLock()
	defer c.Cache.RUnlock()
	for _, chat := range c.Cache.chats {
		if chat.ID == chatID {
			return chat, nil
		}
	}
	chat, err := c.MessagesGetChats([]int64{chatID})
	if err != nil {
		return nil, err
	}
	chatsObj, ok := chat.(*MessagesChatsObj)
	if !ok {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	if len(chatsObj.Chats) == 0 {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	chatObj, ok := chatsObj.Chats[0].(*ChatObj)
	if !ok {
		return nil, fmt.Errorf("no chat with id %d or missing from cache", chatID)
	}
	return chatObj, nil
}

// ----------------- Get User/Channel/Chat from cache -----------------

func (c *Client) GetUser(userID int64) (*UserObj, error) {
	user, err := c.getUserFromCache(userID)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (c *Client) GetChannel(channelID int64) (*Channel, error) {
	channel, err := c.getChannelFromCache(channelID)
	if err != nil {
		return nil, err
	}
	return channel, nil
}

func (c *Client) GetChat(chatID int64) (*ChatObj, error) {
	chat, err := c.getChatFromCache(chatID)
	if err != nil {
		return nil, err
	}
	return chat, nil
}

// ----------------- Update User/Channel/Chat in cache -----------------

func (c *CACHE) UpdateUser(user *UserObj) {
	c.Lock()
	defer c.Unlock()

	c.users[user.ID] = user
	c.InputPeers.InputUsers[user.ID] = user.AccessHash
}

func (c *CACHE) UpdateChannel(channel *Channel) {
	c.Lock()
	defer c.Unlock()

	c.channels[channel.ID] = channel
	c.InputPeers.InputChannels[channel.ID] = channel.AccessHash
}

func (c *CACHE) UpdateChat(chat *ChatObj) {
	c.Lock()
	defer c.Unlock()

	c.chats[chat.ID] = chat
	c.InputPeers.InputChats[chat.ID] = chat.ID
}

func (cache *CACHE) UpdatePeersToCache(u []User, c []Chat) {
	for _, user := range u {
		us, ok := user.(*UserObj)
		if ok {
			cache.UpdateUser(us)
		}
	}
	for _, chat := range c {
		ch, ok := chat.(*ChatObj)
		if ok {
			cache.UpdateChat(ch)
		} else {
			channel, ok := chat.(*Channel)
			if ok {
				cache.UpdateChannel(channel)
			}
		}
	}
}

func (c *Client) GetPeerUser(userID int64) (*InputPeerUser, error) {
	if peer, ok := c.Cache.InputPeers.InputUsers[userID]; ok {
		return &InputPeerUser{UserID: userID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no user with id %d or missing from cache", userID)
}

func (c *Client) GetPeerChannel(channelID int64) (*InputPeerChannel, error) {

	if peer, ok := c.Cache.InputPeers.InputChannels[channelID]; ok {
		return &InputPeerChannel{ChannelID: channelID, AccessHash: peer}, nil
	}
	return nil, fmt.Errorf("no channel with id %d or missing from cache", channelID)
}
