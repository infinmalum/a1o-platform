import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { ListError, ListLoading, ListPages, studioPageSize } from '@/components/StudioList';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { renderQuota } from '@/helpers/render';
import { studioRequest } from '@/helpers/studioRequest';

const categories = ['chat', 'image', 'video', 'audio', 'retrieval', 'other'] as const;

type Catalog = {
  group: string;
  models: {
    name: string;
    category: typeof categories[number];
    groups: string[];
    usable: boolean;
    price?: { input: number; output: number };
  }[];
};

export default function Models() {
  const { t, i18n } = useTranslation();
  const [attempt, setAttempt] = useState(0);
  const [result, setResult] = useState<Catalog | null>();
  const [scope, setScope] = useState('mine');
  const [category, setCategory] = useState('all');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);

  useEffect(() => {
    const request = new AbortController();
    void studioRequest<Catalog>('/api/studio/models', { signal: request.signal, cache: 'no-store' }, false)
      .then((catalog) => { if (!request.signal.aborted) setResult(catalog); })
      .catch(() => { if (!request.signal.aborted) setResult(null); });
    return () => request.abort();
  }, [attempt]);

  const catalog = result?.models ?? [];
  const groups = [...new Set(catalog.flatMap((model) => model.groups))]
    .sort((a, b) => a.localeCompare(b, i18n.resolvedLanguage));
  const search = query.trim().toLowerCase();
  const matches = catalog.filter((model) => (
    (scope === 'all' || (scope === 'mine' ? model.usable : model.groups.includes(scope.slice(6)))) &&
    (category === 'all' || model.category === category) &&
    model.name.toLowerCase().includes(search)
  ));
  const start = page * studioPageSize;

  return (
    <section className='mx-auto w-full max-w-6xl px-4 py-8'>
      <h1 className='mb-5 text-2xl font-semibold tracking-tight'>{t('studio.home.models.title')}</h1>
      {result === undefined && <ListLoading cards />}
      {result === null && <ListError retry={() => { setResult(undefined); setAttempt((current) => current + 1); }} />}
      {result && (
        <>
          <div className='mb-5 grid gap-3 sm:grid-cols-[2fr_1fr_1fr]'>
            <div className='grid min-w-0 gap-2'>
              <Label htmlFor='model-search'>{t('models.search')}</Label>
              <Input id='model-search' type='search' value={query} onChange={(event) => { setQuery(event.target.value); setPage(0); }} />
            </div>
            <div className='grid min-w-0 gap-2'>
              <Label htmlFor='model-scope'>{t('models.scope')}</Label>
              <Select value={scope} onValueChange={(value) => { setScope(value); setPage(0); }}>
                <SelectTrigger id='model-scope' className='w-full'><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value='mine'>{t('models.mine')}</SelectItem>
                  <SelectItem value='all'>{t('models.all')}</SelectItem>
                  {groups.map((group) => <SelectItem key={group} value={'group:' + group}>{t('models.group', { group })}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className='grid min-w-0 gap-2'>
              <Label htmlFor='model-category'>{t('models.category')}</Label>
              <Select value={category} onValueChange={(value) => { setCategory(value); setPage(0); }}>
                <SelectTrigger id='model-category' className='w-full'><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('models.allCategories')}</SelectItem>
                  {categories.map((key) => <SelectItem key={key} value={key}>{t(`models.categories.${key}`)}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>
          <p className='mb-3 text-sm text-muted-foreground' role='status'>
            {t('models.matchCount', { total: matches.length, group: result.group })}
          </p>
          {matches.length === 0 && <p className='rounded-lg border border-dashed px-4 py-8 text-center text-sm'>{t('models.empty')}</p>}
          <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
            {matches.slice(start, start + studioPageSize).map((model) => (
              <Card key={model.name} className='min-w-0 gap-3 py-4'>
                <CardHeader className='gap-2 px-4'>
                  <div className='flex flex-wrap gap-1'>
                    <Badge variant='secondary'>{t(`models.categories.${model.category}`)}</Badge>
                    <Badge variant='outline'>{t(model.usable ? 'models.available' : 'models.restricted')}</Badge>
                  </div>
                  <CardTitle className='font-mono text-sm [overflow-wrap:anywhere]'>{model.name}</CardTitle>
                </CardHeader>
                <CardContent className='grid gap-2 px-4 text-sm'>
                  <p className='text-muted-foreground [overflow-wrap:anywhere]'>{t('models.groups', { groups: model.groups.join(', ') })}</p>
                  {model.price && <p>{t('models.tokenPrice', {
                    input: renderQuota(model.price.input, t, 4), output: renderQuota(model.price.output, t, 4),
                  })}</p>}
                </CardContent>
              </Card>
            ))}
          </div>
          <ListPages page={page} more={start + studioPageSize < matches.length} onPage={setPage} />
        </>
      )}
    </section>
  );
}
