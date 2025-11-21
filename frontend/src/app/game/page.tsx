
'use client';
import { use, useEffect, useState } from 'react';
import React, { useRef } from 'react';
import { Canvas } from '@react-three/fiber';
import {OrbitControls } from '@react-three/drei';
import * as THREE from 'three';
import { useSpring, animated } from '@react-spring/three'
import {RAW_LENGTH, BLOCK_LENGTH, OFFSET_X, OFFSET_Y, COLOR_OF_PLAYER, Player} from '../_utils/helper';
import Chessboard from '../_components/Chessboard';
import { motion, AnimatePresence } from 'framer-motion';
import { useRouter, useSearchParams } from 'next/navigation';
import { SocketContext } from '../_components/socket';
import Loading from './loading';



export default function Home() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const socket = React.useContext(SocketContext);

  const [isClient, setIsClient] = useState(false);
  const [isInit, setIsInit] = useState(false);
  const [gameOver, setGameOver] = useState('');
  
  const [roomId, setRoomId] = useState('');
  const [roomPlayers, setRoomPlayers] = useState([]);
  const [turn, setTurn] = useState(0);
  const [player1, setPlayer1] = useState<Player>({id: 0, name: "player1", chessList: []});
  const [player2, setPlayer2] = useState<Player>({id: 1, name: "player2", chessList: []});
  const [animatedChess, setAnimatedChess] = useState({player: 0, pos: [1, 1], down: false});


  useEffect(() => {
    setIsClient(true);
    
    socket.on('gameOver', (data: any) => {
      console.log('gameOver', data);
      setGameOver(data.winner === socket.id? 'win' : data.winner === 'draw'? 'draw' : 'lose');
    }
    );
    return () => {
      socket.off('updateGame');
      socket.off('gameOver');
    };
  }, []);

  useEffect(() => {
    if (!isInit || socket.id === undefined) {
      return;
    }

    if (roomPlayers[turn] === socket.id) {
      return;
    }
    socket.on('updateGame', (data: any) => {
      console.log('updateGame', data);
      let roomInfo = data.room;
      let allChess = roomInfo.allChess;
      let player1Chess = allChess.filter((chess: any) => chess[0] === 0).map((chess: any) => [chess[1], chess[2]]);
      let player2Chess = allChess.filter((chess: any) => chess[0] === 1).map((chess: any) => [chess[1], chess[2]]);
      setPlayer1({...player1, chessList: player1Chess});
      setPlayer2({...player2, chessList: player2Chess});
      setTurn(prev => roomInfo.turn);
      setAnimatedChess(prev => roomInfo.animatedChess);
    }
    );
    return () => {
      socket.off('updateGame');
      console.log('off updateGame');
    };
  }, [turn, roomPlayers, isInit]);
  useEffect(() => {
    return () => {
      if(isInit) {
        socket.emit('leaveRoom', roomId, (response: any) => {
          console.log('leaveRoom', response);
        }
        );
      }
    }
  }, [isInit, roomId]);
  useEffect(() => {
    const roomId = searchParams.get('roomId');
    if (roomId) {
      console.log('Room ID:', roomId);
      setRoomId(roomId);

      const params = new URLSearchParams(searchParams);
      params.delete('roomId');
      
      router.replace(`/game`);
    }
  }, [searchParams, router]);

  useEffect(() => {
    if (!isClient) {
      return;
    }
    console.log('roomId', roomId);

    // init game by socket
    socket.emit('initGame', roomId, (response: any) => {
      console.log('initGame', response);

      if (response.success) {
        let roomInfo = response.room;
        console.log('initGame success', roomInfo);
        // setPlayer1({...player1, chessList: response.player1.chessList});
        // setPlayer2({...player2, chessList: response.player2.chessList});
        setTurn(prev => roomInfo.turn);
        setAnimatedChess(prev => roomInfo.animatedChess);
        // setChessList 
        let allChess = roomInfo.allChess;
        if (allChess){
          let player1Chess = allChess.filter((chess: any) => chess.player === 0).map((chess: any) => [chess.row, chess.col]);
          let player2Chess = allChess.filter((chess: any) => chess.player === 1).map((chess: any) => [chess.row, chess.col]);
          setPlayer1({...player1, chessList: player1Chess});
          setPlayer2({...player2, chessList: player2Chess});
        }
        setRoomPlayers(roomInfo.players);

        setIsInit(true);
      }else{
        window.alert('initGame failed');
        router.push('/');
      }
    }
    );
  }, [roomId]);


  const handClickChessBoard = (e: any) => {
    if (turn === -1) {
      return;
    }
    if(roomPlayers[turn] !== socket.id) {
      return
    }
    console.log(e);
    if (player1.chessList.some(pos => pos[0] === e.row && pos[1] === e.col) || player2.chessList.some(pos => pos[0] === e.row && pos[1] === e.col)) {
      return;
    }
    if (e.row == animatedChess.pos[0] && e.col == animatedChess.pos[1]) {
      setAnimatedChess({...animatedChess, down: true});
      socket.emit('playerMove', {roomId, moveType: 'animatedChess', ...animatedChess, down: true}, (response: any) => {
        console.log('playerMove', response);
      }
      );
      return;
    }
    if(animatedChess.down) {
      return
    }
    setAnimatedChess({player: turn, pos: [e.row, e.col], down: false});
    socket.emit('playerMove', {roomId, moveType: 'animatedChess', player: turn, pos: [e.row, e.col], down: false}, (response: any) => {
      console.log('playerMove', response);
    } 
    );
  }
  const handleAddChess = async(player: number, pos: [number, number]) => {
    if (turn === -1) {
      return;
    }
    if(roomPlayers[turn] !== socket.id) {
      return
    }
    if (turn === 0){
      setPlayer1(prev => ({ ...prev, chessList: [...prev.chessList, pos] }));
    }else{
      setPlayer2(prev => ({ ...prev, chessList: [...prev.chessList, pos] }));
    }
    // setAnimatedChess({player: -1, pos: [-1, -1], down: false});

    setTurn(prev => 1 - prev);
    socket.emit('playerMove', {roomId, moveType: 'addChess', player, pos}, (response: any) => {
      if (response.success && response.room){
        let roomInfo = response.room;
        let allChess = roomInfo.allChess;
        let player1Chess = allChess.filter((chess: any) => chess.player === 0).map((chess: any) => [chess.row, chess.col]);
        let player2Chess = allChess.filter((chess: any) => chess.player === 1).map((chess: any) => [chess.row, chess.col]);
        setPlayer1({...player1, chessList: player1Chess});
        setPlayer2({...player2, chessList: player2Chess});
        setTurn(prev => roomInfo.turn);
        setAnimatedChess(prev => roomInfo.animatedChess);
      }
    });
  }
  console.log(player1.chessList, '<<< player1.chessList');
  console.log(player2.chessList, '<<< player2.chessList');

  if (!isClient) {
    return null;
  }

  if (!isInit) {
    return <Loading />
  }
  
  return (
    <div style={{height:"100vh"}}>
      <Canvas camera={{ position: [10, 10, 10], fov: 33 }} >
        <ambientLight intensity={1.5}  />
        <directionalLight position={[0, 3, 0]} color={new THREE.Color('white')} intensity={3}/>
        <Chessboard handClickChessBoard={handClickChessBoard} />
        <Chesses player={player1.id} chessList={player1.chessList} />
        <Chesses player={player2.id} chessList={player2.chessList} />
        <AnimatedChess player={animatedChess.player} pos={animatedChess.pos as any} down={animatedChess.down} handleAddChess={handleAddChess} />
        {/* <OrbitControls rotateSpeed={0.3}  enableZoom={true} /> */}
        <OrbitControls rotateSpeed={0.3} minPolarAngle={Math.PI / 8} maxPolarAngle={Math.PI / 3.5} enableZoom={false}  enablePan={false}/>
      </Canvas>
      {gameOver !== '' && 
        <AnimatePresence>
          <Result res={gameOver}/>
        </AnimatePresence>
      }
    </div>
  );
}

function Result({res}: {res: string}) {
  const router = useRouter();
  const handleBackToHome = () => {
    router.push('/');
  }
  return (
    <motion.div
            initial={{ opacity: 0, transform: 'translate(-50%, -50%)', top: '100%'}}
            animate={{ opacity: 0.95, transform: 'translate(-50%, -50%)', top: '50%' }}
            exit={{ opacity: 0, transform: 'translate(-50%, -50%)', top: '100%' }}
            style={{
              position: 'absolute',
              top: '40%',
              left: '50%',
              transform: 'translate(-50%, -50%)',
              background: 'linear-gradient(135deg, #333333, #aaaaaa',
              color: 'white',
              padding: '30px',
              borderRadius: '12px',
              boxShadow: '0 4px 20px rgba(0, 0, 0, 0.3)',
              width: '350px',
              textAlign: 'center',
              fontSize: '30px',
            }}
          >
            <div>
              {res === 'win' ? 'You Win!' : res === 'lose' ? 'You Lose!' : 'Draw!'}
            </div>
            <motion.button
              onClick={handleBackToHome}
              style={{
                marginTop: '20px',
                padding: '10px 20px',
                fontSize: '16px',
                color: '#fff',
                background: '#333',
                border: 'none',
                borderRadius: '8px',
                cursor: 'pointer',
                boxShadow: '0 4px 10px rgba(0, 0, 0, 0.2)',
              }}
              whileHover={{ scale: 1.1 }}
              whileTap={{ scale: 0.9 }}
            >
              Back
            </motion.button>
          </motion.div>
  )
}


function AnimatedChess({player, pos, down, handleAddChess}: {player: number, pos: [number, number], down?: boolean, handleAddChess: any}) {
  const [visible, setVisible] = useState(true);
  const [show, setShow] = useState(true);
  const { position } = useSpring({
    position: down?[pos[1] * BLOCK_LENGTH + OFFSET_X, 1.2, pos[0] * BLOCK_LENGTH +OFFSET_Y]:[pos[1] * BLOCK_LENGTH + OFFSET_X, 2.2, pos[0] * BLOCK_LENGTH +OFFSET_Y],
    config: { mass: 1, tension: 170, friction: 26 },
    onRest: () => {
      if (down) {
        setVisible(false);
        handleAddChess(player, pos);
      }
    }
  });
  useEffect(() => {
    if(!down) {
      setVisible(true);
    }
  }
  , [down]);

  useEffect(() => {
    if(pos[0] == -1 && pos[1] == -1) {
      setShow(false);
    }else {
      setShow(true);
    }

  }
  , [pos]);
  console.log(position.animation.toValues);

  if (!show) {
    return null;
  }
  return (
    <animated.mesh
      position={position as any}
      visible={visible}
    >
      <cylinderGeometry args={[0.3, 0.3, 0.1, 32]} /> {/* 使用圓柱體作為棋子的形狀 */}
      <meshStandardMaterial color={new THREE.Color(COLOR_OF_PLAYER[player])} /> {/* 根據位置設定顏色 */}
    </animated.mesh>
  );
}

function Chesses({player, chessList}: {player: number, chessList: Array<[number, number]>}) {

  return (
    <>
      {chessList.map((pos) => (
        <mesh position={[pos[1] * BLOCK_LENGTH + OFFSET_X, 1.2, pos[0] * BLOCK_LENGTH +OFFSET_Y]} key={`chess${player} ${pos[0]} ${pos[1]}`}>
          <cylinderGeometry args={[0.3, 0.3, 0.1, 32]} /> {/* 使用圓柱體作為棋子的形狀 */}
          <meshStandardMaterial color={new THREE.Color(COLOR_OF_PLAYER[player])} /> {/* 根據位置設定顏色 */}
        </mesh>
      ))}
    </>
  );
}
