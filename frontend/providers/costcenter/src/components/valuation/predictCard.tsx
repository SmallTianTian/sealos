import useBillingStore from '@/stores/billing';
import { displayMoney } from '@/utils/format';
import { Flex } from '@chakra-ui/react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export default function PredictCard() {
  const { t } = useTranslation();
  const total = t('total');
  const state = useBillingStore();
  const leastCost = useMemo(
    () =>
      [
        { name: 'CPU', cost: state.cpu },
        { name: 'Memory', cost: state.memory },
        { name: 'Storage', cost: state.storage },
        { name: total, cost: state.cpu + state.memory + state.storage }
      ].map((item) => ({
        ...item,
        cost: displayMoney(item.cost * 30)
      })),
    [state.cpu, state.memory, state.storage, total]
  );
  return (
    <Flex borderX={'0.5px solid #DEE0E2'}>
      {leastCost.map((item, idx) => (
        <Flex
          key={item.name}
          flex={1}
          position="relative"
          borderX={'0.5px solid #DEE0E2'}
          borderY={'1px solid #DEE0E2'}
          bg={'#F1F4F6'}
          direction={'column'}
        >
          <Flex justify={'center'} align={'center'} h={'42px'} borderBottom={'1px solid #DEE0E2'}>
            {item.name}
          </Flex>
          <Flex justify={'center'} align={'center'} h={'68px'}>
            ￥{item.cost}
          </Flex>
        </Flex>
      ))}
    </Flex>
  );
}
